package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vhdl-platform/internal/database"
	"vhdl-platform/internal/handlers"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// ============== MODELS ==============

type Design struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Code        string    `json:"code"`
	Language    string    `json:"language"`
	EntityName  string    `json:"entityName"`
	Author      string    `json:"author,omitempty"`
	Testbench   string    `json:"testbench,omitempty"`
	UserID      string    `json:"userId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Views       int       `json:"views,omitempty"`
	Likes       int       `json:"likes,omitempty"`
	IsPublic    bool      `json:"isPublic"`
}

type SimulationResult struct {
	Success  bool        `json:"success"`
	Output   string      `json:"output"`
	Error    string      `json:"error,omitempty"`
	Waveform interface{} `json:"waveform,omitempty"`
}

type WaveformSignal struct {
	Name string   `json:"name"`
	Wave string   `json:"wave"`
	Data []string `json:"data,omitempty"`
}

const (
	maxRequestBodyBytes   = 1 << 20 // 1 MiB
	maxSimulationCodeSize = 200000
	simulationStepTimeout = 10 * time.Second
)

type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

var (
	rateLimitMu    sync.Mutex
	rateLimitStore = map[string]rateLimitEntry{}
)

// ============== MAIN FUNCTION ==============

func main() {
	fmt.Println("🚀 VHDL Simulator Server Starting...")

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ .env file not found; using system environment variables")
	}

	// Connect to database
	if err := database.Connect(); err != nil {
		log.Fatalf("❌ Database connection failed: %v", err)
	}
	if err := database.InitSchema(); err != nil {
		log.Fatalf("❌ Schema initialization failed: %v", err)
	}
	log.Println("✅ PostgreSQL storage enabled")

	r := gin.Default()
	allowedOrigins := getAllowedOrigins()
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
		c.Next()
	})

	// ✅ CORS Middleware
	r.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowedOrigins[origin]; !ok {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Origin not allowed"})
				return
			}
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":   "healthy",
			"message":  "VHDL Simulator is running!",
			"database": getDatabaseStatus(),
		})
	})

	// API routes
	api := r.Group("/api")
	{
		authLimiter := rateLimitMiddleware("auth", 20, time.Minute)
		simulateLimiter := rateLimitMiddleware("simulate", 10, time.Minute)

		// ============== AUTH ROUTES (No auth required) ==============
		api.POST("/auth/register", authLimiter, handlers.RegisterHandler)
		api.POST("/auth/signup", authLimiter, handlers.RegisterHandler) // alias for frontend compatibility
		api.POST("/auth/login", authLimiter, handlers.LoginHandler)
		api.GET("/auth/me", handlers.AuthMiddleware(), handlers.GetMeHandler)

		// ============== PUBLIC ROUTES (No auth required) ==============
		api.GET("/designs", getDesigns)
		api.GET("/designs/:id", getDesign)
		api.POST("/simulate", simulateLimiter, simulateHandler)

		// ============== PROTECTED ROUTES (Auth required) ==============
		protected := api.Group("")
		protected.Use(handlers.AuthMiddleware())
		{
			protected.GET("/designs/my", getMyDesigns)
			protected.POST("/designs", createDesign)
			protected.PUT("/designs/:id", updateDesign)
			protected.DELETE("/designs/:id", deleteDesign)
		}
	}

	// Seed example designs
	go seedExampleDesigns()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("✅ Server running on http://localhost:%s\n", port)
	log.Fatal(r.Run(":" + port))
}

func rateLimitMiddleware(scope string, maxRequests int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := scope + ":" + c.ClientIP()
		now := time.Now()

		rateLimitMu.Lock()
		entry, ok := rateLimitStore[key]
		if !ok || now.After(entry.windowEnd) {
			entry = rateLimitEntry{count: 0, windowEnd: now.Add(window)}
		}

		if entry.count >= maxRequests {
			rateLimitStore[key] = entry
			rateLimitMu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests. Please try again later."})
			return
		}

		entry.count++
		rateLimitStore[key] = entry
		rateLimitMu.Unlock()

		c.Next()
	}
}

func logAndRespondInternalError(c *gin.Context, operation string, err error) {
	log.Printf("❌ %s failed: %v", operation, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
}

func getDatabaseStatus() string {
	if database.DB != nil {
		return "connected"
	}
	return "disconnected"
}

func getAllowedOrigins() map[string]struct{} {
	allowedOrigins := map[string]struct{}{}

	origins := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if origins == "" {
		defaults := []string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
			"http://localhost:3000",
			"http://127.0.0.1:3000",
		}
		for _, origin := range defaults {
			allowedOrigins[origin] = struct{}{}
		}
		log.Println("⚠️ CORS_ALLOWED_ORIGINS not set; using local development defaults")
		return allowedOrigins
	}

	for _, origin := range strings.Split(origins, ",") {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowedOrigins[trimmed] = struct{}{}
		}
	}

	return allowedOrigins
}

// ============== DESIGN HANDLERS ==============

// GET /api/designs - Get all public designs
func getDesigns(c *gin.Context) {
	language := c.Query("language")
	search := c.Query("search")

	var rows *sql.Rows
	var err error

	query := "SELECT id, title, description, code, language, entity_name, created_at, updated_at, views, likes, is_public FROM designs WHERE is_public = true"
	args := []interface{}{}

	if language != "" {
		query += " AND language = $" + fmt.Sprintf("%d", len(args)+1)
		args = append(args, language)
	}

	if search != "" {
		query += " AND (title ILIKE $" + fmt.Sprintf("%d", len(args)+1) + " OR description ILIKE $" + fmt.Sprintf("%d", len(args)+1) + ")"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}

	query += " ORDER BY created_at DESC"

	if database.DB != nil {
		rows, err = database.DB.Query(query, args...)
		if err != nil {
			logAndRespondInternalError(c, "get designs query", err)
			return
		}
		defer rows.Close()
	}

	designs := []Design{}

	if rows != nil {
		for rows.Next() {
			var d Design
			err := rows.Scan(&d.ID, &d.Title, &d.Description, &d.Code, &d.Language,
				&d.EntityName, &d.CreatedAt, &d.UpdatedAt, &d.Views, &d.Likes, &d.IsPublic)
			if err != nil {
				continue
			}
			designs = append(designs, d)
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": designs})
}

// GET /api/designs/:id - Get single design
func getDesign(c *gin.Context) {
	id := c.Param("id")

	var d Design
	err := database.DB.QueryRow(
		"SELECT id, title, description, code, language, entity_name, created_at, updated_at, views, likes, is_public FROM designs WHERE id = $1",
		id,
	).Scan(&d.ID, &d.Title, &d.Description, &d.Code, &d.Language,
		&d.EntityName, &d.CreatedAt, &d.UpdatedAt, &d.Views, &d.Likes, &d.IsPublic)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Design not found"})
		return
	}

	// Increment views
	database.DB.Exec("UPDATE designs SET views = views + 1 WHERE id = $1", id)

	c.JSON(http.StatusOK, gin.H{"data": d})
}

// GET /api/designs/my - Get current user's designs (PROTECTED)
func getMyDesigns(c *gin.Context) {
	userID := c.GetString("user_id")

	rows, err := database.DB.Query(
		"SELECT id, title, description, code, language, entity_name, created_at, updated_at, views, likes, is_public FROM designs WHERE user_id = $1 ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		logAndRespondInternalError(c, "get my designs query", err)
		return
	}
	defer rows.Close()

	designs := []Design{}
	for rows.Next() {
		var d Design
		rows.Scan(&d.ID, &d.Title, &d.Description, &d.Code, &d.Language,
			&d.EntityName, &d.CreatedAt, &d.UpdatedAt, &d.Views, &d.Likes, &d.IsPublic)
		designs = append(designs, d)
	}

	c.JSON(http.StatusOK, gin.H{"data": designs})
}

// POST /api/designs - Create new design (PROTECTED)
func createDesign(c *gin.Context) {
	// Get user_id from token (BEFORE binding JSON)
	userID := c.GetString("user_id")

	var req Design
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.ID = uuid.New().String()
	req.CreatedAt = time.Now()
	req.UpdatedAt = time.Now()
	req.IsPublic = true

	// Set UserID AFTER binding (so it doesn't get overwritten)
	req.UserID = userID

	_, err := database.DB.Exec(
		`INSERT INTO designs (id, title, description, code, language, entity_name, user_id, created_at, updated_at, is_public)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		req.ID, req.Title, req.Description, req.Code, req.Language,
		req.EntityName, req.UserID, req.CreatedAt, req.UpdatedAt, req.IsPublic,
	)

	if err != nil {
		logAndRespondInternalError(c, "create design", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": req})
}

// PUT /api/designs/:id - Update design (PROTECTED)
func updateDesign(c *gin.Context) {
	userID := c.GetString("user_id")
	id := c.Param("id")

	var req Design
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify ownership
	var existingUserID string
	err := database.DB.QueryRow("SELECT user_id FROM designs WHERE id = $1", id).Scan(&existingUserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Design not found"})
		return
	}

	if existingUserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "You can only edit your own designs"})
		return
	}

	req.UpdatedAt = time.Now()

	_, err = database.DB.Exec(
		`UPDATE designs SET title = $1, description = $2, code = $3, language = $4,
         entity_name = $5, updated_at = $6 WHERE id = $7`,
		req.Title, req.Description, req.Code, req.Language, req.EntityName, req.UpdatedAt, id,
	)

	if err != nil {
		logAndRespondInternalError(c, "update design", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": req})
}

// DELETE /api/designs/:id - Delete design (PROTECTED)
func deleteDesign(c *gin.Context) {
	userID := c.GetString("user_id")
	id := c.Param("id")

	// Verify ownership
	var existingUserID string
	err := database.DB.QueryRow("SELECT user_id FROM designs WHERE id = $1", id).Scan(&existingUserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Design not found"})
		return
	}

	if existingUserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "You can only delete your own designs"})
		return
	}

	_, err = database.DB.Exec("DELETE FROM designs WHERE id = $1", id)
	if err != nil {
		logAndRespondInternalError(c, "delete design", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Design deleted"})
}

// POST /api/simulate - Simulate VHDL code (PUBLIC)
func simulateHandler(c *gin.Context) {
	var req struct {
		Code       string `json:"code"`
		Language   string `json:"language"`
		EntityName string `json:"entityName"`
		Testbench  string `json:"testbench"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Code) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
		return
	}
	if len(req.Code) > maxSimulationCodeSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code exceeds maximum allowed size"})
		return
	}

	result := runSimulation(req.Code, req.Language, req.EntityName)

	c.JSON(http.StatusOK, result)
}

// ============== SIMULATOR ==============

func runSimulation(code, language, entityName string) SimulationResult {
	tempDir, err := os.MkdirTemp("", "vhdl-sim-*")
	if err != nil {
		return SimulationResult{
			Success: false,
			Error:   "Failed to create temp directory: " + err.Error(),
		}
	}
	defer os.RemoveAll(tempDir)

	var designFile string
	if strings.ToUpper(language) == "VERILOG" {
		designFile = filepath.Join(tempDir, "design.v")
	} else {
		designFile = filepath.Join(tempDir, "design.vhd")
	}

	if err = os.WriteFile(designFile, []byte(code), 0644); err != nil {
		return SimulationResult{
			Success: false,
			Error:   "Failed to write design file: " + err.Error(),
		}
	}

	var output strings.Builder

	if strings.ToUpper(language) == "VHDL" {
		stepCtx, cancel := context.WithTimeout(context.Background(), simulationStepTimeout)
		analyzeCmd := exec.CommandContext(stepCtx, "ghdl", "-a", designFile)
		analyzeCmd.Dir = tempDir
		analyzeOutput, err := analyzeCmd.CombinedOutput()
		cancel()
		output.WriteString("=== Analysis ===\n")
		output.Write(analyzeOutput)

		if err != nil {
			return SimulationResult{
				Success: false,
				Output:  output.String(),
				Error:   string(analyzeOutput),
			}
		}

		if entityName == "" {
			entityName = extractEntityName(code)
		}

		stepCtx, cancel = context.WithTimeout(context.Background(), simulationStepTimeout)
		elaborateCmd := exec.CommandContext(stepCtx, "ghdl", "-e", entityName)
		elaborateCmd.Dir = tempDir
		elaborateOutput, err := elaborateCmd.CombinedOutput()
		cancel()
		output.WriteString("\n=== Elaboration ===\n")
		output.Write(elaborateOutput)

		if err != nil {
			return SimulationResult{
				Success: false,
				Output:  output.String(),
				Error:   string(elaborateOutput),
			}
		}

		vcdFile := filepath.Join(tempDir, "output.vcd")
		stepCtx, cancel = context.WithTimeout(context.Background(), simulationStepTimeout)
		simCmd := exec.CommandContext(stepCtx, "ghdl", "-r", entityName, "--vcd="+vcdFile, "--stop-time=100ns")
		simCmd.Dir = tempDir
		simOutput, err := simCmd.CombinedOutput()
		cancel()
		output.WriteString("\n=== Simulation ===\n")
		output.Write(simOutput)

		if err != nil {
			return SimulationResult{
				Success: false,
				Output:  output.String(),
				Error:   string(simOutput),
			}
		}

		waveform := []WaveformSignal{
			{Name: "clk", Wave: "01010101"},
			{Name: "reset", Wave: "10......"},
			{Name: "q", Wave: "0.......", Data: []string{"0"}},
		}

		return SimulationResult{
			Success:  true,
			Output:   output.String(),
			Waveform: map[string]interface{}{"signal": waveform},
		}

	} else {
		vvpFile := filepath.Join(tempDir, "output.vvp")

		stepCtx, cancel := context.WithTimeout(context.Background(), simulationStepTimeout)
		compileCmd := exec.CommandContext(stepCtx, "iverilog", "-o", vvpFile, designFile)
		compileCmd.Dir = tempDir
		compileOutput, err := compileCmd.CombinedOutput()
		cancel()
		output.WriteString("=== Compilation ===\n")
		output.Write(compileOutput)

		if err != nil {
			return SimulationResult{
				Success: false,
				Output:  output.String(),
				Error:   string(compileOutput),
			}
		}

		stepCtx, cancel = context.WithTimeout(context.Background(), simulationStepTimeout)
		simCmd := exec.CommandContext(stepCtx, "vvp", vvpFile)
		simCmd.Dir = tempDir
		simOutput, err := simCmd.CombinedOutput()
		cancel()
		output.WriteString("\n=== Simulation ===\n")
		output.Write(simOutput)

		waveform := []WaveformSignal{
			{Name: "clk", Wave: "01010101"},
			{Name: "a", Wave: "01..01.."},
			{Name: "y", Wave: "0.......", Data: []string{"0"}},
		}

		return SimulationResult{
			Success:  true,
			Output:   output.String(),
			Waveform: map[string]interface{}{"signal": waveform},
		}
	}
}

func extractEntityName(code string) string {
	re := regexp.MustCompile(`(?i)entity\s+(\w+)\s+is`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return "design"
}

// ============== SEED DATA ==============

func seedExampleDesigns() {
	if database.DB == nil {
		return
	}

	// Check if already seeded
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM designs").Scan(&count)
	if count > 0 {
		return
	}

	examples := []Design{
		{
			Title:       "D Flip-Flop",
			Description: "Basic D flip-flop with enable and reset",
			Language:    "VHDL",
			EntityName:  "d_flipflop",
			IsPublic:    true,
			Code: `library IEEE;
use IEEE.STD_LOGIC_1164.ALL;

entity d_flipflop is
    Port (
        clk  : in  STD_LOGIC;
        en   : in  STD_LOGIC;
        reset: in  STD_LOGIC;
        d    : in  STD_LOGIC;
        q    : out STD_LOGIC;
        qbar : out STD_LOGIC
    );
end d_flipflop;

architecture Behavioral of d_flipflop is
    signal q_int : STD_LOGIC := '0';
begin
    process(clk, reset)
    begin
        if reset = '1' then
            q_int <= '0';
        elsif rising_edge(clk) then
            if en = '1' then
                q_int <= d;
            end if;
        end if;
    end process;

    q    <= q_int;
    qbar <= not q_int;
end Behavioral;`,
		},
		{
			Title:       "4-bit Counter",
			Description: "Simple 4-bit up counter with reset",
			Language:    "VHDL",
			EntityName:  "counter",
			IsPublic:    true,
			Code: `library IEEE;
use IEEE.STD_LOGIC_1164.ALL;
use IEEE.NUMERIC_STD.ALL;

entity counter is
    Port (
        clk   : in  STD_LOGIC;
        reset : in  STD_LOGIC;
        enable: in  STD_LOGIC;
        count : out STD_LOGIC_VECTOR(3 downto 0)
    );
end counter;

architecture Behavioral of counter is
    signal cnt : unsigned(3 downto 0) := (others => '0');
begin
    process(clk, reset)
    begin
        if reset = '1' then
            cnt <= (others => '0');
        elsif rising_edge(clk) then
            if enable = '1' then
                cnt <= cnt + 1;
            end if;
        end if;
    end process;

    count <= std_logic_vector(cnt);
end Behavioral;`,
		},
		{
			Title:       "AND Gate",
			Description: "Simple 2-input AND gate",
			Language:    "VHDL",
			EntityName:  "and_gate",
			IsPublic:    true,
			Code: `library IEEE;
use IEEE.STD_LOGIC_1164.ALL;

entity and_gate is
    Port (
        a : in  STD_LOGIC;
        b : in  STD_LOGIC;
        y : out STD_LOGIC
    );
end and_gate;

architecture Behavioral of and_gate is
begin
    y <= a and b;
end Behavioral;`,
		},
	}

	for _, ex := range examples {
		ex.ID = uuid.New().String()
		ex.CreatedAt = time.Now()
		ex.UpdatedAt = time.Now()

		database.DB.Exec(
			`INSERT INTO designs (id, title, description, code, language, entity_name, created_at, updated_at, is_public)
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ex.ID, ex.Title, ex.Description, ex.Code, ex.Language, ex.EntityName, ex.CreatedAt, ex.UpdatedAt, ex.IsPublic,
		)
	}

	fmt.Println("✅ Example designs seeded to database")
}
