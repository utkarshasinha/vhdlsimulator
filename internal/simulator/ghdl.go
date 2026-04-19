package simulator

import (
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strings"
)

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
        // GHDL analyze
        analyzeCmd := exec.Command("ghdl", "-a", designFile)
        analyzeCmd.Dir = tempDir
        analyzeOutput, err := analyzeCmd.CombinedOutput()
        output.WriteString("=== Analysis ===\n")
        output.Write(analyzeOutput)
        
        if err != nil {
            return SimulationResult{
                Success: false,
                Output:  output.String(),
                Error:   string(analyzeOutput),
            }
        }
        
        // Extract entity name if not provided
        if entityName == "" {
            entityName = extractEntityName(code)
        }
        
        // GHDL elaborate
        elaborateCmd := exec.Command("ghdl", "-e", entityName)
        elaborateCmd.Dir = tempDir
        elaborateOutput, err := elaborateCmd.CombinedOutput()
        output.WriteString("\n=== Elaboration ===\n")
        output.Write(elaborateOutput)
        
        if err != nil {
            return SimulationResult{
                Success: false,
                Output:  output.String(),
                Error:   string(elaborateOutput),
            }
        }
        
        // GHDL simulate
        vcdFile := filepath.Join(tempDir, "output.vcd")
        simCmd := exec.Command("ghdl", "-r", entityName, "--vcd="+vcdFile, "--stop-time=100ns")
        simCmd.Dir = tempDir
        simOutput, err := simCmd.CombinedOutput()
        output.WriteString("\n=== Simulation ===\n")
        output.Write(simOutput)
        
        if err != nil {
            return SimulationResult{
                Success: false,
                Output:  output.String(),
                Error:   string(simOutput),
            }
        }
        
        // Generate sample waveform
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
        // Verilog - Icarus Verilog
        vvpFile := filepath.Join(tempDir, "output.vvp")
        
        compileCmd := exec.Command("iverilog", "-o", vvpFile, designFile)
        compileCmd.Dir = tempDir
        compileOutput, err := compileCmd.CombinedOutput()
        output.WriteString("=== Compilation ===\n")
        output.Write(compileOutput)
        
        if err != nil {
            return SimulationResult{
                Success: false,
                Output:  output.String(),
                Error:   string(compileOutput),
            }
        }
        
        simCmd := exec.Command("vvp", vvpFile)
        simCmd.Dir = tempDir
        simOutput, err := simCmd.CombinedOutput()
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