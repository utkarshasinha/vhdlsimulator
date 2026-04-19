package models

import "time"

type Design struct {
    ID          string    `json:"id"`
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Code        string    `json:"code"`
    Language    string    `json:"language"` // "vhdl" or "verilog"
    EntityName  string    `json:"entity_name"`
    Testbench   string    `json:"testbench"`
    WaveformData string   `json:"waveform_data,omitempty"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    Views       int       `json:"views"`
    Likes       int       `json:"likes"`
}

type Simulation struct {
    ID        string    `json:"id"`
    DesignID  string    `json:"design_id"`
    InputData string    `json:"input_data"`
    Output    string    `json:"output"`
    Waveform  string    `json:"waveform"`
    Success   bool      `json:"success"`
    Error     string    `json:"error,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}