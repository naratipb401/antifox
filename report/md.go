package report

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"antifox/db"
)

// mdLeadNumRe strips the DOCX-layout section-number prefix (e.g. "3.1 ") from
// system titles — the MD deliverable is standalone and numbers sections from 1.
var mdLeadNumRe = regexp.MustCompile(`^\d+(\.\d+)*\s+`)

// BuildMarkdown renders the delivery-grade Markdown report body plus the
// evidence-image manifest (relative name → on-disk source path).
// Layout mirrors the manual delivery template: info header → grouped by target →
// per-finding (property table + 7 numbered subsections) → footer. Evidence images
// are referenced via the relative path assets/<name> and packaged into the same
// zip by GenerateMarkdownFile (no more base64 inlining — keeps the .md clean and
// small, directly editable in Typora/VSCode with relative image resolution).
func BuildMarkdown(ctx context.Context, pg *db.DB, in *GenInput) (string, map[string]string) {
	doc := BuildDoc(ctx, pg, in)
	loc := doc.Meta.Locale
	L := func(zh, tw, en string) string { return pick(loc, zh, tw, en) }
	assets := map[string]string{}
	var b strings.Builder

	// ── info header ──
	b.WriteString("# " + doc.Meta.ReportTitle + "\n\n")
	b.WriteString("> **" + L("Prepared by", "Prepared by", "Prepared by") + "**: " + doc.Meta.CompanyName + "  \n")
	b.WriteString("> **" + L("Date", "Date", "Date") + "**: " + doc.Meta.Date +
		" · **" + L("Version", "Version", "Version") + "**: " + doc.Meta.Version + "  \n")
	var crit, high, med, low int
	for _, s := range doc.Summary.Systems {
		crit += s.Crit
		high += s.High
		med += s.Med
		low += s.Low
	}
	b.WriteString("> **" + L("Findings", "Findings", "Findings") + "**: " +
		fmt.Sprintf("%s %d · %s %d · %s %d · %s %d (%s %d)\n",
			L("Critical", "Critical", "Critical"), crit, L("High", "High", "High"), high,
			L("Medium", "Medium", "Medium"), med, L("Low", "Low", "Low"), low,
			L("total", "total", "total"), crit+high+med+low))
	b.WriteString("\n---\n\n")

	s1 := L("Description", "Description", "Description")
	s3 := L("Module", "Module", "Module")
	s4 := L("Testing Process", "Testing Process", "Testing Process")
	s5 := L("Risk Analysis", "Risk Analysis", "Risk Analysis")
	s6 := L("Remediation", "Remediation", "Remediation")
	s7 := L("Remediation Status", "Remediation Status", "Remediation Status")
	tTarget := L("Target", "Target", "Target")
	tRisk := L("Severity", "Severity", "Severity")
	tDate := L("Found", "Found", "Found")

	sysIdx := 0
	for _, sysd := range doc.SystemsDetail {
		sysIdx++
		b.WriteString(fmt.Sprintf("## %d %s\n\n", sysIdx, mdLeadNumRe.ReplaceAllString(sysd.Name, "")))
		vulnIdx := 0
		for _, v := range sysd.Vulns {
			vulnIdx++
			// heading: <sys>.<vuln> <name> (<severity>/CVSS score <score>) — numbered from 1
			paren := v.Severity
			if v.CVSSScore > 0 {
				paren += "/" + L("CVSS score ", "CVSS score ", "CVSS score ") + fmt.Sprintf("%.1f", v.CVSSScore)
			}
			b.WriteString(fmt.Sprintf("### %d.%d %s (%s)\n\n", sysIdx, vulnIdx, v.Title, paren))
			// property table
			b.WriteString("| | |\n|---|---|\n")
			b.WriteString("| **" + tTarget + "** | " + mdCell(nz(v.Target, v.URL)) + " |\n")
			b.WriteString("| **" + tRisk + "** | **" + v.Severity + "** |\n")
			b.WriteString("| **" + tDate + "** | " + v.FoundDate + " |\n\n")
			// 1. description
			b.WriteString("**1. " + s1 + "**\n\n")
			for _, p := range v.Description {
				b.WriteString(p + "\n\n")
			}
			// 2. URL
			b.WriteString("**2. URL**\n\n")
			b.WriteString(nz(v.URL, v.Target) + "\n\n")
			// 3. module
			b.WriteString("**3. " + s3 + "**\n\n")
			b.WriteString(defaultStr(v.Module, "—") + "\n\n")
			// 4. testing process (step prose + verbatim request/response + evidence screenshots)
			b.WriteString("**4. " + s4 + "**\n\n")
			for _, blk := range v.TestProcess {
				switch blk.Type {
				case "code":
					b.WriteString("```\n" + strings.TrimRight(blk.Content, "\n") + "\n```\n\n")
				case "image":
					if blk.Path != "" && fileExists(blk.Path) {
						rel := filepath.Base(blk.Path)
						assets[rel] = blk.Path
						b.WriteString("![" + mdEscapeAlt(blk.Caption) + "](assets/" + rel + ")\n")
						if blk.Caption != "" {
							b.WriteString("*" + blk.Caption + "*\n")
						}
						b.WriteString("\n")
					}
				default:
					b.WriteString(blk.Content + "\n\n")
				}
			}
			// 5. risk analysis
			b.WriteString("**5. " + s5 + "**\n\n")
			for _, r := range v.RiskAnalysis {
				b.WriteString("- " + r + "\n")
			}
			b.WriteString("\n")
			// 6. remediation
			b.WriteString("**6. " + s6 + "**\n\n")
			for _, r := range v.Remediation {
				b.WriteString("- " + r + "\n")
			}
			b.WriteString("\n")
			// 7. remediation status
			b.WriteString("**7. " + s7 + "**\n\n")
			b.WriteString(v.FixStatus + "\n\n")
			b.WriteString("---\n\n")
		}
	}
	b.WriteString("*" + fmt.Sprintf(
		L("Report prepared by %s on %s.", "Report prepared by %s on %s.", "Report prepared by %s on %s."),
		doc.Meta.CompanyName, doc.Meta.Date) + "*\n")
	return b.String(), assets
}

// GenerateMarkdownFile renders the MD deliverable zip (report.md + assets/ evidence
// images) and returns the zip path. Pure Go, no python dependency; images ship as
// real files referenced relatively, readable/editable in Typora/VSCode after unzip.
func GenerateMarkdownFile(ctx context.Context, pg *db.DB, in *GenInput, workDir, taskID string) (string, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create report dir: %w", err)
	}
	md, assets := BuildMarkdown(ctx, pg, in)
	zipPath := filepath.Join(workDir, fmt.Sprintf("task-%s-%s.zip", safeName(taskID), nowStamp(ctx)))
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to create report archive: %w", err)
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)
	mw, err := zw.Create("report.md")
	if err != nil {
		return "", fmt.Errorf("failed to create report.md in archive: %w", err)
	}
	if _, err := mw.Write([]byte(md)); err != nil {
		return "", fmt.Errorf("failed to write report.md: %w", err)
	}
	for rel, src := range assets {
		data, err := os.ReadFile(src)
		if err != nil {
			continue // skip missing images; never fail the whole package
		}
		aw, err := zw.Create("assets/" + rel)
		if err != nil {
			continue
		}
		_, _ = aw.Write(data)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize archive: %w", err)
	}
	return zipPath, nil
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// mdCell escapes characters that would break the markdown table inside a cell.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// mdEscapeAlt sanitizes image alt text (strips characters that break ![]() syntax).
func mdEscapeAlt(s string) string {
	s = strings.ReplaceAll(s, "]", "")
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.TrimSpace(s) == "" {
		return "evidence"
	}
	return s
}
