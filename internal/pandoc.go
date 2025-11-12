package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// HasPandoc returns nil if pandoc is available on PATH, otherwise an error.
func HasPandoc() error {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return fmt.Errorf("pandoc not found: %w", err)
	}
	return nil
}

// ConvertMarkdownToDOCX converts a Markdown file at mdPath to a DOCX at outPath.
// The H1 in the Markdown serves as the document title; no metadata title is set
// to avoid duplicate titles when imported into Google Docs.
func ConvertMarkdownToDOCX(ctx context.Context, mdPath, outPath string) error {
	if err := HasPandoc(); err != nil {
		return err
	}
	args := []string{"-f", "gfm", "-t", "docx", "-o", outPath, mdPath}
	cmd := exec.CommandContext(ctx, "pandoc", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pandoc docx failed: %v: %s", err, string(out))
	}
	return nil
}

// pickPDFEngine attempts to find a preferred PDF engine. Returns empty string
// if none is found; pandoc will fall back to its defaults which may require a
// TeX engine present.
func pickPDFEngine() string {
	// Prefer LaTeX-based engines for typographic control; wkhtmltopdf last.
	for _, eng := range []string{"tectonic", "xelatex", "lualatex", "pdflatex", "wkhtmltopdf"} {
		if _, err := exec.LookPath(eng); err == nil {
			return eng
		}
	}
	return ""
}

// ConvertMarkdownToPDFWithEngine allows specifying a preferred PDF engine.
// If engine is empty or not found, it falls back to pickPDFEngine().
func ConvertMarkdownToPDFWithEngine(ctx context.Context, mdPath, outPath, engine string) error {
	if err := HasPandoc(); err != nil {
		return err
	}
	eng := engine
	if eng != "" {
		if _, err := exec.LookPath(eng); err != nil {
			eng = ""
		}
	}
	if eng == "" {
		eng = pickPDFEngine()
	}
	args := []string{"-f", "gfm", "-t", "pdf", "-o", outPath, mdPath}
	if eng != "" {
		args = append(args, "--pdf-engine="+eng)
	}
	var headerFile string
	if eng == "tectonic" || eng == "pdflatex" || eng == "xelatex" || eng == "lualatex" {
		font := os.Getenv("TESS_PDF_SANS_FONT")
		if font == "" {
			switch runtime.GOOS {
			case "darwin":
				font = "Helvetica Neue"
			case "windows":
				font = "Arial"
			default:
				font = "Noto Sans"
			}
		}
		// Instruct pandoc's LaTeX template to use the sans font as the main font.
		args = append(args, "-V", "mainfont="+font, "-V", "sansfont="+font, "-V", "familydefault=sf")
		f, err := os.CreateTemp("", "tess-pandoc-header-*.tex")
		if err == nil {
			_, _ = f.WriteString("\\usepackage{fontspec}\n\\setmainfont{" + font + "}\n\\setsansfont{" + font + "}\n\\renewcommand{\\familydefault}{\\sfdefault}\n")
			f.Close()
			headerFile = f.Name()
			args = append(args, "-H", headerFile)
			defer os.Remove(headerFile)
		}
	}
	cmd := exec.CommandContext(ctx, "pandoc", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pandoc pdf failed: %v: %s", err, string(out))
	}
	return nil
}

// ConvertMarkdownToPDF converts a Markdown file at mdPath to a PDF at outPath.
// It tries to select a reasonable PDF engine if available.
func ConvertMarkdownToPDF(ctx context.Context, mdPath, outPath string) error {
	return ConvertMarkdownToPDFWithEngine(ctx, mdPath, outPath, "")
}
