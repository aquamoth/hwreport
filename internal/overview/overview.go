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
	DetailHref    template.URL
	ReportDate    string
	CPUModel      string
	CPUMark       *int
	MemoryGB      *float64
	DriveGB       float64
	SmartStatus   string
	SmartSeverity int
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

type versionLink struct {
	Label   string
	Href    template.URL
	Current bool
}

type detailPageData struct {
	Identifier     string
	Version        string
	GeneratedAt    string
	SourceJSONHref template.URL
	SourceJSONName string
	PassMarkURL    template.URL
	PreviousHref   template.URL
	PreviousLabel  string
	NextHref       template.URL
	NextLabel      string
	Versions       []versionLink
	Report         *report.Report
	PrettyJSON     string
}

type sourceRecord struct {
	Report      *report.Report
	Identifier  string
	SourcePath  string
	ReportLabel string
	CollectedAt time.Time
	Row         row
	DetailPath  string
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

	detailDir := filepath.Join(filepath.Dir(outputPath), "hwreport-details")
	if err := os.MkdirAll(detailDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create detail directory: %w", err)
	}
	if err := clearDetailHTML(detailDir); err != nil {
		return Result{}, err
	}

	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return Result{}, fmt.Errorf("read input directory: %w", err)
	}

	var records []sourceRecord
	totalFiles := 0
	ctx := context.Background()
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}

		sourcePath := filepath.Join(inputDir, entry.Name())
		reportData, err := loadReport(sourcePath)
		if err != nil {
			continue
		}
		if reportData.SchemaVersion == 0 {
			continue
		}
		totalFiles++

		currentRow := summarizeReport(reportData)

		if cpuModel := strings.TrimSpace(pointerString(reportData.CPU.Model)); cpuModel != "" {
			if lookup, err := passmarkClient.Lookup(ctx, cpuModel); err == nil {
				currentRow.CPUMark = lookup.CPUMark
				currentRow.PassMarkURL = safeURL(lookup.LookupURL)
			}
		}

		identifier := currentRow.Identifier
		collectedAt := reportCollectedAt(reportData, sourcePath)
		detailPath := filepath.Join(detailDir, detailFileName(entry.Name()))
		currentRow.DetailHref = fileHref(outputPath, detailPath)
		currentRow.ReportDate = formatReportDate(collectedAt)

		records = append(records, sourceRecord{
			Report:      reportData,
			Identifier:  identifier,
			SourcePath:  sourcePath,
			ReportLabel: entry.Name(),
			CollectedAt: collectedAt,
			Row:         currentRow,
			DetailPath:  detailPath,
		})
	}

	if len(records) == 0 {
		return Result{}, fmt.Errorf("no readable hwreport JSON files found in %s", inputDir)
	}

	grouped := map[string][]*sourceRecord{}
	for idx := range records {
		record := &records[idx]
		grouped[record.Identifier] = append(grouped[record.Identifier], record)
	}

	var rows []row
	for _, versions := range grouped {
		sort.Slice(versions, func(i, j int) bool {
			if versions[i].CollectedAt.Equal(versions[j].CollectedAt) {
				return strings.ToUpper(versions[i].ReportLabel) < strings.ToUpper(versions[j].ReportLabel)
			}
			return versions[i].CollectedAt.Before(versions[j].CollectedAt)
		})

		for idx, record := range versions {
			if err := writeDetailPage(record, versions, idx, options); err != nil {
				return Result{}, err
			}
		}

		rows = append(rows, versions[len(versions)-1].Row)
	}

	sort.Slice(rows, func(i, j int) bool {
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

func writeDetailPage(record *sourceRecord, versions []*sourceRecord, currentIndex int, options Options) error {
	prettyJSONBytes, err := json.MarshalIndent(record.Report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report json for detail page: %w", err)
	}

	versionLinks := make([]versionLink, 0, len(versions))
	for idx, version := range versions {
		versionLinks = append(versionLinks, versionLink{
			Label:   versionLabel(version),
			Href:    fileHref(record.DetailPath, version.DetailPath),
			Current: idx == currentIndex,
		})
	}

	data := detailPageData{
		Identifier:     record.Identifier,
		Version:        options.Version,
		GeneratedAt:    options.Now.UTC().Format(time.RFC3339),
		SourceJSONHref: fileHref(record.DetailPath, record.SourcePath),
		SourceJSONName: filepath.Base(record.SourcePath),
		PassMarkURL:    record.Row.PassMarkURL,
		Versions:       versionLinks,
		Report:         record.Report,
		PrettyJSON:     string(prettyJSONBytes),
	}
	if len(versions) > 1 {
		previousIndex := currentIndex - 1
		if previousIndex < 0 {
			previousIndex = len(versions) - 1
		}
		nextIndex := currentIndex + 1
		if nextIndex >= len(versions) {
			nextIndex = 0
		}

		data.PreviousHref = fileHref(record.DetailPath, versions[previousIndex].DetailPath)
		data.PreviousLabel = versionLabel(versions[previousIndex])
		data.NextHref = fileHref(record.DetailPath, versions[nextIndex].DetailPath)
		data.NextLabel = versionLabel(versions[nextIndex])
	}

	rendered, err := renderDetailPage(data)
	if err != nil {
		return err
	}

	if err := os.WriteFile(record.DetailPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write detail page: %w", err)
	}
	return nil
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

func renderDetailPage(data detailPageData) ([]byte, error) {
	tmpl, err := template.New("detail").Funcs(template.FuncMap{
		"fmtGB": func(value *float64) string {
			if value == nil {
				return ""
			}
			return trimFloat(*value)
		},
		"fmtInt": func(value *int) string {
			if value == nil {
				return ""
			}
			return fmt.Sprintf("%d", *value)
		},
		"fmtUint32": func(value *uint32) string {
			if value == nil {
				return ""
			}
			return fmt.Sprintf("%d", *value)
		},
		"fmtString": func(value *string) string {
			return pointerString(value)
		},
		"fmtPixels": func(width, height *uint32) string {
			if width == nil || height == nil {
				return ""
			}
			return fmt.Sprintf("%d x %d", *width, *height)
		},
		"fmtPhysical": func(width, height *float64, unit *string) string {
			if width == nil || height == nil {
				return ""
			}
			unitValue := pointerString(unit)
			if unitValue == "" {
				return fmt.Sprintf("%s x %s", trimFloat(*width), trimFloat(*height))
			}
			return fmt.Sprintf("%s x %s %s", trimFloat(*width), trimFloat(*height), unitValue)
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
	}).Parse(detailTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse detail template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render detail template: %w", err)
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

func detailFileName(sourceJSONName string) string {
	base := strings.TrimSuffix(sourceJSONName, filepath.Ext(sourceJSONName))
	return base + ".html"
}

func clearDetailHTML(detailDir string) error {
	entries, err := os.ReadDir(detailDir)
	if err != nil {
		return fmt.Errorf("read detail directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".html" {
			continue
		}
		if err := os.Remove(filepath.Join(detailDir, entry.Name())); err != nil {
			return fmt.Errorf("remove stale detail page %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func reportCollectedAt(reportData *report.Report, sourcePath string) time.Time {
	if reportData != nil {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(reportData.CollectedAtUTC)); err == nil {
			return parsed
		}
	}
	if info, err := os.Stat(sourcePath); err == nil {
		return info.ModTime().UTC()
	}
	return time.Time{}
}

func versionLabel(record *sourceRecord) string {
	label := formatReportDate(record.CollectedAt)
	return label + " - " + record.ReportLabel
}

func formatReportDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
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
    th[aria-sort="ascending"] .sort-button::after {
      content: " ▲";
    }
    th[aria-sort="descending"] .sort-button::after {
      content: " ▼";
    }
    .sort-button {
      padding: 0;
      border: 0;
      background: transparent;
      color: inherit;
      font: inherit;
      letter-spacing: inherit;
      text-transform: inherit;
      cursor: pointer;
    }
  </style>
</head>
<body>
  <main>
    <h1>Hardware Overview</h1>
    <p class="meta">Generated {{ .GeneratedAt }} from {{ .RenderedRows }} report(s) in {{ .InputDir }}. Version {{ .Version }}.</p>
    <div class="panel">
      <table id="overview-table">
        <thead>
          <tr>
            <th aria-sort="ascending" data-sort-type="text"><button type="button" class="sort-button">Computer</button></th>
            <th aria-sort="none" data-sort-type="text"><button type="button" class="sort-button">Date</button></th>
            <th aria-sort="none" data-sort-type="text"><button type="button" class="sort-button">CPU</button></th>
            <th aria-sort="none" data-sort-type="number"><button type="button" class="sort-button">CPU Mark</button></th>
            <th aria-sort="none" data-sort-type="number"><button type="button" class="sort-button">RAM GB</button></th>
            <th aria-sort="none" data-sort-type="number"><button type="button" class="sort-button">Drive GB</button></th>
            <th aria-sort="none" data-sort-type="text"><button type="button" class="sort-button">SMART</button></th>
          </tr>
        </thead>
        <tbody>
          {{ range .Rows }}
          <tr data-row-key="{{ .Identifier }}">
            <td data-sort-value="{{ .Identifier }}"><a href="{{ .DetailHref }}">{{ .Identifier }}</a></td>
            <td data-sort-value="{{ .ReportDate }}">{{ .ReportDate }}</td>
            <td class="cpu" data-sort-value="{{ .CPUModel }}">
              <div>{{ .CPUModel }}</div>
              {{ if .PassMarkURL }}<div class="subtle"><a href="{{ .PassMarkURL }}">PassMark source</a></div>{{ end }}
            </td>
            <td class="numeric" data-sort-value="{{ fmtInt .CPUMark }}">{{ fmtInt .CPUMark }}</td>
            <td class="numeric" data-sort-value="{{ fmtGB .MemoryGB }}">{{ fmtGB .MemoryGB }}</td>
            <td class="numeric" data-sort-value="{{ fmtDrive .DriveGB }}">{{ fmtDrive .DriveGB }}</td>
            <td data-sort-value="{{ .SmartStatus }}"><span class="smart-badge {{ smartClass .SmartStatus }}">{{ .SmartStatus }}</span></td>
          </tr>
          {{ end }}
        </tbody>
      </table>
    </div>
  </main>
  <script>
    (function () {
      const table = document.getElementById('overview-table');
      if (!table || !table.tBodies.length || !table.tHead) {
        return;
      }

      const tbody = table.tBodies[0];
      const headers = Array.from(table.tHead.rows[0].cells);

      function rowKey(row) {
        return (row.getAttribute('data-row-key') || '').toLocaleLowerCase();
      }

      function readValue(row, columnIndex, sortType) {
        const cell = row.cells[columnIndex];
        if (!cell) {
          return '';
        }

        const rawValue = (cell.getAttribute('data-sort-value') || cell.textContent || '').trim();
        if (sortType === 'number') {
          if (rawValue === '') {
            return null;
          }
          const numberValue = Number(rawValue.replace(/,/g, ''));
          return Number.isFinite(numberValue) ? numberValue : null;
        }

        return rawValue.toLocaleLowerCase();
      }

      function compareRows(leftRow, rightRow, columnIndex, sortType, direction) {
        const leftValue = readValue(leftRow, columnIndex, sortType);
        const rightValue = readValue(rightRow, columnIndex, sortType);
        const leftEmpty = leftValue === null || leftValue === '';
        const rightEmpty = rightValue === null || rightValue === '';

        if (leftEmpty && rightEmpty) {
          return rowKey(leftRow).localeCompare(rowKey(rightRow));
        }
        if (leftEmpty) {
          return 1;
        }
        if (rightEmpty) {
          return -1;
        }

        let comparison = 0;
        if (sortType === 'number') {
          comparison = leftValue - rightValue;
        } else {
          comparison = leftValue.localeCompare(rightValue);
        }

        if (comparison === 0) {
          comparison = rowKey(leftRow).localeCompare(rowKey(rightRow));
        }

        return direction === 'ascending' ? comparison : -comparison;
      }

      headers.forEach(function (headerCell) {
        const button = headerCell.querySelector('.sort-button');
        if (!button) {
          return;
        }

        button.addEventListener('click', function () {
          const columnIndex = headerCell.cellIndex;
          const sortType = headerCell.getAttribute('data-sort-type') || 'text';
          const nextDirection = headerCell.getAttribute('aria-sort') === 'ascending' ? 'descending' : 'ascending';
          const rows = Array.from(tbody.rows);

          rows.sort(function (leftRow, rightRow) {
            return compareRows(leftRow, rightRow, columnIndex, sortType, nextDirection);
          });

          rows.forEach(function (row) {
            tbody.appendChild(row);
          });

          headers.forEach(function (cell) {
            cell.setAttribute('aria-sort', 'none');
          });
          headerCell.setAttribute('aria-sort', nextDirection);
        });
      });
    }());
  </script>
</body>
</html>
`

const detailTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Identifier }} - Hardware Detail</title>
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
      max-width: 1200px;
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
    .actions {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-bottom: 24px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 18px;
      padding: 18px 20px;
      margin-bottom: 18px;
      box-shadow: 0 14px 36px rgba(53, 48, 36, 0.08);
    }
    h2 {
      margin: 0 0 12px;
      font-size: 18px;
      letter-spacing: -0.02em;
    }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    th, td {
      padding: 10px 12px;
      text-align: left;
      border-bottom: 1px solid #ece6d8;
      vertical-align: top;
      font-size: 14px;
    }
    tr:last-child th, tr:last-child td {
      border-bottom: none;
    }
    th {
      width: 240px;
      color: var(--muted);
      font-weight: 600;
    }
    .list-table th {
      width: auto;
      text-transform: uppercase;
      font-size: 12px;
      letter-spacing: 0.08em;
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
    pre {
      margin: 0;
      overflow: auto;
      background: #f7f3e9;
      border: 1px solid #e8e0cf;
      border-radius: 12px;
      padding: 16px;
      font-size: 13px;
      line-height: 1.45;
    }
    a {
      color: var(--accent);
      text-decoration: none;
    }
    a:hover {
      text-decoration: underline;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{ .Identifier }}</h1>
    <p class="meta">Generated {{ .GeneratedAt }}. Version {{ .Version }}.</p>
    <div class="actions">
      <a href="{{ .SourceJSONHref }}">Source JSON</a>
      {{ if .PassMarkURL }}<a href="{{ .PassMarkURL }}">PassMark CPU source</a>{{ end }}
      {{ if .PreviousHref }}<a href="{{ .PreviousHref }}">Previous version: {{ .PreviousLabel }}</a>{{ end }}
      {{ if .NextHref }}<a href="{{ .NextHref }}">Next version: {{ .NextLabel }}</a>{{ end }}
    </div>

    <section class="panel">
      <h2>Versions</h2>
      <table class="list-table">
        <tr><th>Snapshot</th><th>Open</th></tr>
        {{ range .Versions }}
        <tr>
          <td>{{ .Label }}</td>
          <td>{{ if .Current }}Current{{ else }}<a href="{{ .Href }}">Open</a>{{ end }}</td>
        </tr>
        {{ end }}
      </table>
    </section>

    <section class="panel">
      <h2>System</h2>
      <table>
        <tr><th>Hostname</th><td>{{ .Report.Hostname }}</td></tr>
        <tr><th>Computer</th><td>{{ fmtString .Report.Computer.Manufacturer }} {{ fmtString .Report.Computer.Model }}</td></tr>
        <tr><th>OS</th><td>{{ fmtString .Report.OS.Name }} {{ fmtString .Report.OS.Version }}</td></tr>
        <tr><th>CPU</th><td>{{ fmtString .Report.CPU.Model }}</td></tr>
        <tr><th>Memory</th><td>{{ fmtGB .Report.Memory.TotalInstalledGB }} GB</td></tr>
      </table>
    </section>

    <section class="panel">
      <h2>Storage</h2>
      <table class="list-table">
        <tr><th>Model</th><th>Type</th><th>Size GB</th><th>SMART</th></tr>
        {{ range .Report.Storage }}
        <tr>
          <td>{{ fmtString .Model }}</td>
          <td>{{ fmtString .Type }}</td>
          <td>{{ fmtGB .SizeGB }}</td>
          <td><span class="smart-badge {{ smartClass (fmtString .SmartStatus) }}">{{ fmtString .SmartStatus }}</span></td>
        </tr>
        {{ end }}
      </table>
    </section>

    <section class="panel">
      <h2>Monitors</h2>
      <table class="list-table">
        <tr><th>Model</th><th>Pixels</th><th>Physical Size</th><th>Rotation</th></tr>
        {{ range .Report.Monitors }}
        <tr>
          <td>{{ fmtString .Manufacturer }} {{ fmtString .Model }}</td>
          <td>{{ fmtPixels .PixelWidth .PixelHeight }}</td>
          <td>{{ fmtPhysical .PhysicalWidth .PhysicalHeight .PhysicalUnit }}</td>
          <td>{{ fmtInt .RotationDegrees }}</td>
        </tr>
        {{ end }}
      </table>
    </section>

    <section class="panel">
      <h2>Raw JSON</h2>
      <pre>{{ .PrettyJSON }}</pre>
    </section>
  </main>
</body>
</html>
`
