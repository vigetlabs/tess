# Tess

Tess is a small CLI that pulls review information from the Lattice API and helps you assemble a clean report for a specific direct report and review cycle. It provides an interactive TUI to pick a report and cycle, generates a Markdown summary (peer feedback and self review), and can optionally upload a document to Google Drive using rclone (with pandoc for conversion).

## Features

- Interactive selection of a direct report (Bubble Tea)
- Fetches review cycles and all review responses for the selected person
- Groups feedback by question; decodes HTML entities and compacts blank lines
- Generates a local Markdown file titled `firstname_lastname_cycle_name.md`
- Optional upload to Google Drive as a native Google Doc (DOCX import) or as a PDF

## Install / Build

- Requirements: Go 1.24.x
- Build: `make`
- Run: `./bin/tess`

## Configuration

- Tess looks for a TOML file containing an API key.
- Default location: `~/.tess/config.toml`
- You can override with `--config`.

Example `~/.tess/config.toml`:

```
api_key = "Bearer <your_lattice_api_key>"
# Optional: default rclone remote name (CLI flag overrides)
rclone_remote = "drive"
```

Note: If your key is not prefixed, Tess will add `Bearer ` automatically.

## Usage

Run Tess, pick a direct report and a review cycle. Tess writes a Markdown file and (optionally) uploads a document to Drive:

```
./bin/tess \
  --config ~/.tess/config.toml \
  --rclone-folder-id 1Zte6JSoXX-L3vHiehI56spri8N_dsyuO
```

The interactive UI supports:
- Up/Down or j/k to move
- Enter to select
- q or Ctrl+C to quit

## Flags

- `--config`: Path to config TOML (default: `~/.tess/config.toml`).
- `--rclone-remote`: rclone remote name (default: `drive`).
- `--rclone-folder-id`: Google Drive folder ID. If present, Tess uploads the final report.
- `--upload-format`: `docx` (default, imports as a Google Doc) or `pdf` (uploads a PDF file as-is).
- `--pdf-engine`: Preferred PDF engine for pandoc (e.g., `tectonic`, `xelatex`). Leave empty for auto.

Config precedence: if `rclone_remote` is present in `config.toml`, Tess uses it unless the `--rclone-remote` flag is provided, in which case the flag wins.

Notes:

- If `--rclone-folder-id` is omitted, no rclone upload is attempted.
- The uploaded Doc (DOCX import) is titled `[Reviewee Name] ([Review Cycle])` and is placed directly in the folder with the given ID (no extra subfolder).

## Google Drive Upload (rclone + pandoc)

Tess uses rclone to send the final document to Drive. For a native Google Doc, Tess converts the generated Markdown into a DOCX using pandoc, then asks Drive to import it as a Google Doc. For PDFs, Tess renders a PDF with pandoc and uploads it as a regular file.

### Requirements

- `rclone` installed and configured with a Google Drive remote (default remote name is `drive`).
- `pandoc` installed for Markdown → DOCX/PDF conversion.
- For PDFs: a PDF engine. Tess auto-detects available engines and prefers LaTeX-based ones. The recommended lightweight option is `tectonic`.

### DOCX (Google Doc import)

- Tess runs: `pandoc -f gfm -t docx -o <doc>.docx <input>.md`
- Uploads with: `rclone copyto <doc>.docx <remote>:<Title> --drive-root-folder-id=<FOLDER_ID> --drive-import-formats=docx`

### PDF

- Tess runs: `pandoc -f gfm -t pdf -o <doc>.pdf <input>.md --pdf-engine=<ENGINE>`
- Engine selection: auto-detected; you can force with `--pdf-engine tectonic` (or `xelatex`, etc.).
- On LaTeX engines (including Tectonic), Tess sets a sans‑serif main font by default. Override with `TESS_PDF_SANS_FONT="Inter"` if you prefer a specific font installed on your system.
- Uploads with: `rclone copyto <doc>.pdf <remote>:<Title>.pdf --drive-root-folder-id=<FOLDER_ID>`

### Quick install tips

- macOS: `brew install rclone pandoc tectonic`
- Ubuntu/Debian: `sudo apt-get install rclone pandoc` and install Tectonic via `sudo apt-get install tectonic` or the upstream installer
- Windows: installers from rclone.org and pandoc.org; Tectonic via `winget install Tectonic.Tectonic` or the MSI

### Configure rclone for Drive (once)

1. `rclone config`
2. `n` (new remote), name it `drive`
3. Storage: `drive`
4. Accept defaults, allow auto browser auth
5. (Optional) If your target folder is under Shared items, pass `--drive-root-folder-id=<ID>` at upload time (Tess does this for you)

## Troubleshooting

- 401 Unauthorized: Confirm your `api_key` is valid (and, if missing `Bearer `, Tess adds it automatically).
- rclone cannot find remote: Ensure `rclone config` created a Drive remote and that `--rclone-remote` matches.
- Pandoc not found: Install pandoc or remove `--rclone-folder-id` to skip upload.
- For PDFs: if the result looks serif, specify a font installed on your system, e.g. `TESS_PDF_SANS_FONT="Helvetica"` and/or force an engine via `--pdf-engine tectonic`.
- Conversion mismatch errors: DOCX import usually behaves best for Google Docs. If you still see mismatches, ensure there isn’t an existing Google Doc with the exact same title in the folder; remove it and retry.

## What Tess Does Under The Hood

- Default config path resolution and TOML parsing for `api_key`
- `GET /v1/me` and list direct reports
- `GET /v1/reviewCycles`, then filter cycles by the selected user’s reviewee list
- `GET /v1/reviewee/.../reviews?limit=100`
- Resolve reviewer names and question text (with basic caching)
- Generate Markdown with Peer Feedback and Self Review sections
- Optional: pandoc + rclone upload to Drive as a native Google Doc or PDF

## License

This project is internal-use; no license specified.
