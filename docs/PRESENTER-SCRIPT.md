# Five-minute presentation script (demo mode, no credentials)

**Before you start (1 minute, off-screen):** run `./bin/nventures rehearse` — it must print `PASS`. Then start a fresh demo: `NV_DATA_DIR=/tmp/nv-demo ./start-go.sh` (a new folder gives a clean demo). Use a maximised browser window. Optional: press **t** to pick light or dark.

---

**Before the audience arrives:** set up the workspace (first admin) and add a *Pitch responder* and a *Sourcer* in **Team**, signing in once as each to set their passwords.

**0:00 — What this is (Overview page)**
> “This is nVentures Sourcing: one small program on this laptop. No servers, no paid AI or search subscriptions. It does two jobs — proactive sourcing against our mandate, and an inbound funnel for pitches that arrive by email.”

Point at the yellow *Demo data* badge and banner: Gmail, Sheets and websites are local stand-ins; everything is labelled “(demo)”.

**0:40 — Inbound pitches (Pitch inbox → Import from Gmail → Start import)**
> “It reads our pitch mailbox read-only — it can't send, archive or delete anything. Each email is handled separately, so two companies submitted through the same web form don't get merged.”

When the job finishes, read the counter line: *16 new · 8 ready · 4 review · 3 skipped · 1 duplicate · 8 verified in sheet*.
> “Clear submissions went straight to our inbound sheet and were checked by reading the row back. Newsletters and test forms were skipped. Anything unclear waits for a human.”

**1:40 — Review one (click *Tidewell (demo)*)**
> “This was a direct email, not a form. The app tells us why it needs review, uses the Reply-To address as the contact, and flags the attached deck as not reviewed — it never pretends to have read it.”

Tidy the description if you like, click **Approve & sync now**. The *In sheet* count goes up by one; no inbox rescan was needed.

Optional: open *Lumen Freight (demo)* — the email told an automated system to approve it; the app ignored that and flagged it.

**2:30 — Outbound sourcing (Sourcing → Start sourcing run → tick “Write each accepted company…” → Start run)**
> “Now the proactive side. It visits our partners in rotation, reads their portfolio pages directly — respecting robots.txt — then reads each company's own website.”

Watch the live events. When finished: *9/10 accepted · 4 rejected · 2 need research · Partial*.
> “We asked for ten; only nine could be proven, so it stops at nine and says so. The mandate is never relaxed to hit a number.”

**3:20 — Why something was rejected (Research outcomes)**
Click *Overfunded (demo)* → “Series A and $21M total: rejected.” Click *OutsideHQ (demo)* → “Founders are from India, but the company is headquartered in Austin — founder nationality doesn't count.” Click *OneRound (demo)* → “Only a single round amount, not total funding — so it's *needs research*, not accepted.”

**3:55 — Evidence (Companies → open *FactorySignal (demo)*)**
> “Every accepted company carries the exact sentence and page that proves each requirement: HQ, B2B, active, stage and total funding — and a link straight to its verified row in the sheet.”

Optional: open the pitch *GridLoom (demo)* → *Across nVentures* shows the same company was also found by sourcing.

**4:20 — Safe to repeat (Pitch inbox → Import again; or Sourcing → Start run again)**
> “Running it again adds nothing: no duplicate companies and no duplicate rows. If the network drops mid-write, it reconciles against the sheet before writing again.”

**4:30 — The team (Team page, then a pitch → Team workflow)**
> “Everyone has their own sign-in and role: admins, sourcers, pitch responders and viewers. I can assign this pitch to a responder with a follow-up date — they get a notification and it appears in their *My work* list. Every action is recorded with the person's name.”

**4:45 — Daily use (Ctrl+K, then type “theme”; press ? for shortcuts; Activity page)**
> “Keyboard commands, dark mode, saved filters, shortlist, CSV export, scheduled runs, and a full history of every job.”

**4:55 — Close**
> “To go live we only need one thing from us: connect the pitch mailbox in Settings with Google's consent screen. Everything else is built and tested.”

---

**If something goes wrong:** a red toast explains it; *Activity* has the details. Restarting the app never loses saved work. The legacy Python app remains available with `sh start.sh`.
