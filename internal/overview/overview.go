package overview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"specreport/internal/passmark"
	"specreport/internal/report"
)

type Options struct {
	InputDir   string
	OutputPath string
	Now        time.Time
	Version    string
}

type Result struct {
	OutputPath string
}

type row struct {
	Identifier    string
	CPUModel      string
	CPUMark       *int
	MemoryGB      *float64
	DriveGB       float64
	SmartStatus   string
	SmartSeverity int
	ReportHref    template.URL
	ReportLabel   string
	PassMarkURL   template.URL
}

type pageData struct {
	GeneratedAt  string
	InputDir     string
	Version      string
	Rows         []row
	TotalFiles   int
	RenderedRows int
}

func Generate(options Options) (Result, error) {
	inputDir, err := filepath.Abs(options.InputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve input directory: %w", err)
	}

	outputPath := options.OutputPath
	if strings.TrimSpace(outputPath) == "" {
		outputPath = filepath.Join(inputDir, "hwreport-overview.html")
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output path: %w", err)
	}

	cachePath := filepath.Join(inputDir, ".hwoverview-passmark-cache.json")
	passmarkClient, err := passmark.NewClient(cachePath)
	if err != nil {
		return Result{}, err
	}

	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return Result{}, fmt.Errorf("read input directory: %w", err)
	}

	var rows []row
	totalFiles := 0
	ctx := context.Background()
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		totalFiles++

		sourcePath := filepath.Join(inputDir, entry.Name())
		reportData, err := loadReport(sourcePath)
		if err != nil {
			continue
		}

		currentRow := summarizeReport(reportData)
		currentRow.ReportLabel = entry.Name()
		currentRow.ReportHref = fileHref(outputPath, sourcePath)

		if cpuModel := strings.TrimSpace(pointerString(reportData.CPU.Model)); cpuModel != "" {
			if lookup, err := passmarkClient.Lookup(ctx, cpuModel); err == nil {
				currentRow.CPUMark = lookup.CPUMark
				currentRow.PassMarkURL = safeURL(lookup.LookupURL)
			}
		}

		rows = append(rows, currentRow)
	}

	if len(rows) == 0 {
		return Result{}, fmt.Errorf("no readable hwreport JSON files found in %s", inputDir)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SmartSeverity != rows[j].SmartSeverity {
			return rows[i].SmartSeverity > rows[j].SmartSeverity
		}
		leftScore := cpuMarkSortValue(rows[i].CPUMark)
		rightScore := cpuMarkSortValue(rows[j].CPUMark)
		if leftScore != rightScore {
			return leftScore < rightScore
		}
		return strings.ToUpper(rows[i].Identifier) < strings.ToUpper(rows[j].Identifier)
	})

	data := pageData{
		GeneratedAt:  options.Now.UTC().Format(time.RFC3339),
		InputDir:     inputDir,
		Version:      options.Version,
		Rows:         rows,
		TotalFiles:   totalFiles,
		RenderedRows: len(rows),
	}

	rendered, err := renderPage(data)
	if err != nil {
		return Result{}, err
	}

	if err := os.WriteFile(outputPath, rendered, 0o644); err != nil {
		return Result{}, fmt.Errorf("write overview html: %w", err)
	}

	return Result{OutputPath: outputPath}, nil
}

func loadReport(path string) (*report.Report, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data report.Report
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func summarizeReport(data *report.Report) row {
	identifier := strings.TrimSpace(data.Hostname)
	if identifier == "" {
		identifier = strings.TrimSpace(pointerString(data.Computer.Model))
	}
	if identifier == "" {
		identifier = "unknown"
	}

	var totalDriveGB float64
	worstStatus := ""
	worstSeverity := -1
	for _, drive := range data.Storage {
		if drive.SizeGB != nil {
			totalDriveGB += *drive.SizeGB
		}
		status := strings.TrimSpace(pointerString(drive.SmartStatus))
		severity := smartSeverity(status)
		if severity > worstSeverity {
			worstSeverity = severity
			worstStatus = status
		}
	}
	if worstStatus == "" {
		worstStatus = "unknown"
		worstSeverity = smartSeverity(worstStatus)
	}

	return row{
		Identifier:    identifier,
		CPUModel:      strings.TrimSpace(pointerString(data.CPU.Model)),
		MemoryGB:      data.Memory.TotalInstalledGB,
		DriveGB:       totalDriveGB,
		SmartStatus:   worstStatus,
		SmartSeverity: worstSeverity,
	}
}

func renderPage(data pageData) ([]byte, error) {
	tmpl, err := template.New("overview").Funcs(template.FuncMap{
		"fmtGB": func(value *float64) string {
			if value == nil {
				return ""
			}
			return trimFloat(*value)
		},
		"fmtDrive": func(value float64) string {
			if value <= 0 {
				return ""
			}
			return trimFloat(value)
		},
		"fmtInt": func(value *int) string {
			if value == nil {
				return ""
			}
			return fmt.Sprintf("%d", *value)
		},
		"smartClass": func(value string) string {
			switch smartSeverity(value) {
			case 3:
				return "smart-error"
			case 2:
				return "smart-warning"
			case 1:
				return "smart-unknown"
			default:
				return "smart-ok"
			}
		},
	}).Parse(pageTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse overview template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render overview template: %w", err)
	}

	return buf.Bytes(), nil
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func smartSeverity(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error":
		return 3
	case "warning":
		return 2
	case "ok":
		return 0
	case "unavailable", "unknown", "":
		return 1
	default:
		return 1
	}
}

func cpuMarkSortValue(value *int) int {
	if value == nil {
		return 1<<30 - 1
	}
	return *value
}

func trimFloat(value float64) string {
	text := fmt.Sprintf("%.2f", value)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	return text
}

func fileHref(outputPath, sourcePath string) template.URL {
	outputDir := filepath.Dir(outputPath)
	rel, err := filepath.Rel(outputDir, sourcePath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return safeURL((&url.URL{Path: filepath.ToSlash(rel)}).String())
	}

	absolute, absErr := filepath.Abs(sourcePath)
	if absErr != nil {
		return ""
	}
	return safeURL((&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(absolute)}).String())
}

func safeURL(value string) template.URL {
	return template.URL(value)
}

const pageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Hardware Overview</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4f2ea;
      --panel: #fffdf7;
      --ink: #1f241f;
      --muted: #5f665f;
      --line: #d8d2c3;
      --accent: #184f3d;
      --warn: #9c6a00;
      --bad: #8f1d1d;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", "Aptos", sans-serif;
      color: var(--ink);
      background: linear-gradient(180deg, #f6f3ea 0%, #ece7dc 100%);
    }
    main {
      max-width: 1400px;
      margin: 0 auto;
      padding: 32px 24px 48px;
    }
    h1 {
      margin: 0 0 8px;
      font-size: 32px;
      letter-spacing: -0.04em;
    }
    p.meta {
      margin: 0 0 20px;
      color: var(--muted);
      font-size: 14px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 18px;
      overflow: hidden;
      box-shadow: 0 14px 36px rgba(53, 48, 36, 0.08);
    }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    thead th {
      padding: 14px 16px;
      text-align: left;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      background: #f8f4ea;
      border-bottom: 1px solid var(--line);
    }
    tbody td {
      padding: 14px 16px;
      border-bottom: 1px solid #ece6d8;
      vertical-align: top;
      font-size: 14px;
    }
    tbody tr:last-child td {
      border-bottom: none;
    }
    td.numeric {
      text-align: right;
      white-space: nowrap;
      font-variant-numeric: tabular-nums;
    }
    td.cpu {
      min-width: 280px;
    }
    .smart-badge {
      display: inline-block;
      min-width: 84px;
      padding: 6px 10px;
      border-radius: 999px;
      text-align: center;
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    .smart-ok {
      color: var(--accent);
      background: rgba(24, 79, 61, 0.12);
    }
    .smart-unknown {
      color: var(--muted);
      background: rgba(95, 102, 95, 0.12);
    }
    .smart-warning {
      color: var(--warn);
      background: rgba(156, 106, 0, 0.12);
    }
    .smart-error {
      color: var(--bad);
      background: rgba(143, 29, 29, 0.12);
    }
    a {
      color: var(--accent);
      text-decoration: none;
    }
    a:hover {
      text-decoration: underline;
    }
    .subtle {
      color: var(--muted);
      font-size: 12px;
    }
  </style>
</head>
<body>
  <main>
    <h1>Hardware Overview</h1>
    <p class="meta">Generated {{ .GeneratedAt }} from {{ .RenderedRows }} report(s) in {{ .InputDir }}. Version {{ .Version }}.</p>
    <div class="panel">
      <table>
        <thead>
          <tr>
            <th>Computer</th>
            <th>CPU</th>
            <th>CPU Mark</th>
            <th>RAM GB</th>
            <th>Drive GB</th>
            <th>Worst SMART</th>
            <th>Report</th>
          </tr>
        </thead>
        <tbody>
          {{ range .Rows }}
          <tr>
            <td>{{ .Identifier }}</td>
            <td class="cpu">
              <div>{{ .CPUModel }}</div>
              {{ if .PassMarkURL }}<div class="subtle"><a href="{{ .PassMarkURL }}">PassMark source</a></div>{{ end }}
            </td>
            <td class="numeric">{{ fmtInt .CPUMark }}</td>
            <td class="numeric">{{ fmtGB .MemoryGB }}</td>
            <td class="numeric">{{ fmtDrive .DriveGB }}</td>
            <td><span class="smart-badge {{ smartClass .SmartStatus }}">{{ .SmartStatus }}</span></td>
            <td><a href="{{ .ReportHref }}">{{ .ReportLabel }}</a></td>
          </tr>
          {{ end }}
        </tbody>
      </table>
    </div>
  </main>
</body>
</html>
`
