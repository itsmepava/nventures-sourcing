# nVentures Workspace v0.5.0-alpha.1 — pitch-deck reading (prerelease)

Builds on v0.4.0-alpha.1 (team workspace MVP). The v0.4.0-alpha.1 prerelease was prepared but never published, so this release also carries everything listed in its notes. Still one Go executable with the same two direct dependencies (`modernc.org/sqlite`, `golang.org/x/net`). The deck reader uses only the Go standard library. Existing databases upgrade automatically (schema 3 → 4); nothing is rewritten.

## New

- **Read pitch decks on request.** In the pitch review panel, **Read deck text** downloads a PDF or PowerPoint (.pptx) attachment read-only from Gmail and extracts its text on this computer. No AI service, upload or extra software is involved. Pitch responders and admins can read decks; viewers and sourcers see the button locked, with the reason shown.
- **Facts with sources.** The reading lists facts found in the deck: description, HQ, website, contact, stage, funding to date, current raise, founders, founding year and traction. Each fact shows its page number and the exact sentence it came from. **Use** copies a fact into the review form and **Add to notes** appends the quoted line. Nothing is saved until the reviewer saves, and reading a deck never changes a pitch's status or sheet row.
- **Searchable deck text**, page by page (or slide by slide), with *Find in deck*.
- **Clear outcomes when a deck can't be read:** *Password-protected*, *No text layer* (scanned or image-only), *Too large* (over 20 MB), *Not readable* (legacy .ppt, Keynote, other files) or *Damaged / unreadable*. Each comes with a one-line explanation.
- **Deck text is data.** An instruction written in a deck is never followed. The demo Tidewell deck asks "automated reviewers" to approve it, and the pitch still waits for a person.
- Every reading is audited (file name, status and page count, never the deck's contents) and attributed to the person who asked for it. Reading again replaces the previous reading.
- **Demo and rehearsal.** The demo mailbox now includes a generated PDF deck (Tidewell) and PPTX deck (Byte Bazaar). `nventures rehearse` reads the Tidewell deck and approves the pitch using the HQ evidenced on page 2.

## Partners without a website

- Partner lists can use a **`Company Portfolio`** column (a link to the fund's portfolio page) instead of `Website`. A deep link is treated as the company list itself.
- **When the link is blank, nVentures finds the website** before crawling: first the partner contact's work-email domain, then a short list of addresses built from the fund's name. A site is accepted only if the page names the partner and is about investing; parked and for-sale domains are rejected.
- **Optional Tavily search**, like the old app, for partners the free methods can't find. It is off by default and needs `TAVILY_API_KEY` in `.env`, live mode, and **Settings → Search the web (Tavily) for missing partner websites**. Only the partner's name is sent, and requests are counted in Settings.
- **Found websites are marked "found — confirm"** on the Partners page, with how they were found, and can be confirmed or corrected. The sheet's partner list is never modified. A failed lookup is retried after 7 days, or on demand with **Find website**.
- **Websites for the sheet (CSV)** on the Partners page lists the links your partner list lacks, with status and how each was found, ready to paste into `Company Portfolio`. Cells are formula-neutralized.
- **The team's sourcing tab layout is supported:** `source` and `Updates (sourcing sheet)` are filled; analyst, valuation, raise and founder-email columns stay blank; without a `Total Funding USD` column, the evidenced total goes into `Extra Notes`.
- Schema 5 adds where each partner website came from (upgrades automatically).

## How the reader works

- **PDF:** a bounded object parser that does not depend on cross-reference tables. It handles object streams, Flate/LZW/ASCII85/ASCIIHex/RunLength filters, ToUnicode maps, simple and composite (Identity-H) fonts, glyph-name encodings, glyph widths (for word spacing), Form XObjects and inline images.
- **PPTX:** slides in presentation order (not file-name order), tables one row per line, SmartArt text.
- **Limits:** 20 MB input, 16 MB per decompressed stream, 96 MB decompressed per file, 200,000 objects, 400 pages, nesting depth 64, 5 million content tokens and 300,000 characters of kept text.

## Fixed

- At 375 px the top bar was wider than the screen, so every page scrolled sideways. The page title now shrinks instead.
- Opening `#/demo-sheet` without a sheet showed an error. It now offers the two demo sheets.

## Verified (13 Nov 2026, demo fixtures, Claude Code cloud session, Go 1.27.4)

- `go vet` clean, and `go test ./...` passes in all packages, including the new `deck` package.
- Real decks produced by LibreOffice (PPTX → PDF), reportlab (base-14 and embedded TrueType), Chromium (Skia) and python-pptx extract the expected text in order, cross-checked against `pdftotext`. An encrypted PDF reports *Password-protected*, and an image-only PDF reports *No text layer*.
- Synthetic PDFs cover object streams with an XRef-stream trailer, composite fonts with bfchar/bfrange maps, filter chains, glyph-name differences, TJ kerning spaces, self-referencing forms, page-tree and reference cycles, deep nesting and a 64 MB decompression bomb.
- About 1.2 million mutated inputs ran with panic recovery disabled and a 3-second watchdog per input. Two parser bugs were found and fixed (an out-of-range position at an unterminated hex string, and an integer overflow from a huge `/Length`); both inputs are kept as regression tests.
- HTTP tests cover the full role × endpoint matrix (now including deck reading), reading both demo decks with the expected facts, the pitch unchanged after reading, re-reading replacing the stored reading, unknown/unsupported attachments, live mode without Google (clear 409) and oversize attachments (recorded as too large without downloading).
- The schema upgrade test runs on a schema-1 database through to schema 4; readings attach to existing pitches and are deleted with them.
- `-race` is clean for httpapp, jobs, store, discovery and deck.
- `nventures rehearse` passes.
- Browser QA (Chromium):
  - Reading the PDF and PPTX demo decks works.
  - *Use* fills the HQ field and *Add to notes* appends the quoted line.
  - *Find in deck* works, and the unsaved-change guard triggers after using a fact.
  - Light, dark, desktop and 375 px layouts were checked.
  - All 10 pages at 375 px and 1360 px, in light and dark, show no sideways scrolling, no error states and no console errors.

## Not verified / limits

- Website guessing and Tavily search were tested against local fixtures only (fake sites and a fake Tavily server). No live Tavily request has been made.
- Live Gmail and Sheets are still unverified because no Google credentials are configured. Attachment download uses Gmail's documented read-only `messages.attachments.get` and was tested against the local fixture only.
- No OCR: scanned or image-only decks are reported, not read. Deck links (DocSend, Drive, Pitch, …) are not fetched because they usually need the founder's sign-in.
- Facts are pattern-based suggestions for a human reviewer. They can miss phrasing the patterns don't cover, and they are never applied automatically.
- Windows and macOS builds are cross-compiled but not run.
