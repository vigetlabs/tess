package main

import (
    "bufio"
    "errors"
    "flag"
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

func defaultConfigPath() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(home, ".tess", "config.toml"), nil
}

func loadAPIKeyFromTOML(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return "", fmt.Errorf("config file not found: %s", path)
        }
        return "", err
    }
    defer f.Close()

    var apiKey string
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := scanner.Text()
        // Strip comments and whitespace
        if i := strings.Index(line, "#"); i >= 0 {
            line = line[:i]
        }
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "[") { // ignore sections
            continue
        }
        // Parse simple key = value pairs
        parts := strings.SplitN(line, "=", 2)
        if len(parts) != 2 {
            continue
        }
        key := strings.TrimSpace(parts[0])
        val := strings.TrimSpace(parts[1])
        // Trim surrounding quotes if present
        val = strings.Trim(val, " \t")
        if len(val) >= 2 {
            if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
                val = val[1 : len(val)-1]
            }
        }
        if key == "api_key" {
            apiKey = val
            break
        }
    }
    if err := scanner.Err(); err != nil {
        return "", err
    }
    if strings.TrimSpace(apiKey) == "" {
        return "", fmt.Errorf("missing 'api_key' in config: %s", path)
    }
    return apiKey, nil
}

func main() {
    cfgFlag := flag.String("config", "", "Path to config TOML (default: ~/.tess/config.toml)")
    flag.Parse()

    var cfgPath string
    if *cfgFlag != "" {
        cfgPath = *cfgFlag
    } else {
        var err error
        cfgPath, err = defaultConfigPath()
        if err != nil {
            fmt.Fprintf(os.Stderr, "error determining default config path: %v\n", err)
            os.Exit(1)
        }
    }

    apiKey, err := loadAPIKeyFromTOML(cfgPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "%v\n", err)
        os.Exit(1)
    }

    // Placeholder: prove we loaded config successfully (do not print secrets).
    _ = apiKey
    fmt.Println("hello, world")
}

