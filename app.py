from dotenv import load_dotenv
load_dotenv()

import os
import json
import time
from datetime import datetime, timezone
import streamlit as st

import database
from auth import require_login
from config import settings, ELIGIBLE_COUNTRIES, FREE_OPENROUTER_MODEL
from google_sheets import get_worksheets
from sourcing_engine import run_sourcing

st.set_page_config(
    page_title="nVentures Sourcing",
    page_icon="🚀",
    layout="wide",
)

database.init_db()

# First-run admin bootstrap.  Set ADMIN_EMAIL and ADMIN_PASSWORD in secrets.
admin_email = os.getenv("ADMIN_EMAIL", "")
admin_password = os.getenv("ADMIN_PASSWORD", "")
if admin_email and admin_password:
    database.ensure_admin(admin_email, admin_password)

user = require_login()

st.sidebar.title("nVentures")
st.sidebar.caption(user["email"])
if st.sidebar.button("Sign out"):
    st.session_state.user = None
    st.rerun()

page = st.sidebar.radio(
    "Navigate",
    ["Dashboard", "Run History", "Admin"],
)

if page == "Dashboard":
    st.title("AI Company Sourcing")
    st.caption("South Asia + Singapore • Google Sheets output • AI verification")
    st.caption(f"OpenRouter: {settings.openrouter_model} • free-only")

    c1, c2, c3 = st.columns(3)
    with c1:
        target = st.number_input("Target new companies", min_value=1, max_value=100, value=25)
    with c2:
        max_partners = st.number_input(
            "Partners per run", min_value=1, max_value=50, value=settings.max_partners
        )
    with c3:
        max_research = st.number_input(
            "Deep research limit", min_value=1, max_value=200, value=settings.max_deep_research
        )

    st.markdown("### Geography")
    st.info("Hard filter: " + ", ".join(ELIGIBLE_COUNTRIES))

    st.markdown("### Investment criteria")
    st.write(
        f"**B2B:** required  •  **Stage:** pre-seed/seed  •  "
        f"**Approx. total funding ceiling:** ${settings.max_total_funding:,.0f}"
    )

    st.warning(
        "Companies headquartered outside the eligible geography are rejected "
        "in Python even if the AI says they qualify."
    )

    if st.button("🚀 Start sourcing", type="primary", use_container_width=True):
        started = datetime.now(timezone.utc).isoformat()
        progress = st.progress(0)
        status = st.empty()

        try:
            sh, sourcing_ws, partner_ws, control_ws, partner_name = get_worksheets(
                settings.spreadsheet_id,
                settings.sourcing_tab,
                settings.control_tab,
                settings.partner_tab_candidates,
            )

            status.info("Connected to Google Sheets. Starting sourcing run...")

            def on_progress(value, message=""):
                try:
                    progress.progress(max(0, min(100, int(value))))
                except Exception:
                    pass
                if message:
                    status.write(message)

            report = run_sourcing(
                sourcing_ws=sourcing_ws,
                partner_ws=partner_ws,
                control_ws=control_ws,
                openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
                tavily_api_key=os.getenv("TAVILY_API_KEY", ""),
                # FREE-ONLY: always use OpenRouter's free-model router.
                openrouter_model=FREE_OPENROUTER_MODEL,
                target_companies=int(target),
                max_partners=int(max_partners),
                max_deep_research=int(max_research),
                max_candidates_per_partner=settings.max_candidates_per_partner,
                max_total_funding=settings.max_total_funding,
                max_team_size_warning=settings.max_team_size_warning,
                tavily_timeout=settings.tavily_timeout,
                openrouter_timeout=settings.openrouter_timeout,
                max_tavily_results=settings.max_tavily_results,
                max_research_chars=settings.max_research_chars,
                request_delay=settings.request_delay,
                progress_callback=on_progress,
            )

            finished = datetime.now(timezone.utc).isoformat()
            run_id = database.save_run(user["email"], started, finished, int(target), report)
            progress.progress(100)
            status.success(f"Run #{run_id} completed.")

            accepted = report.get("accepted", [])
            rejected = report.get("rejected", [])
            duplicates = report.get("duplicates", [])
            errors = report.get("partner_errors", [])

            a, b, c, d = st.columns(4)
            a.metric("Added", len(accepted))
            b.metric("Duplicates", len(duplicates))
            c.metric("Rejected", len(rejected))
            d.metric("Partner errors", len(errors))

            if accepted:
                st.markdown("### New companies")
                st.dataframe(report.get("accepted_details", accepted), use_container_width=True)

            if rejected:
                with st.expander(f"Rejected ({len(rejected)})"):
                    st.write(rejected)

            if duplicates:
                with st.expander(f"Duplicates ({len(duplicates)})"):
                    st.write(duplicates)

            if errors:
                with st.expander(f"Partner/API errors ({len(errors)})"):
                    st.write(errors)

        except Exception as exc:
            st.error("The sourcing run failed before completion.")
            st.exception(exc)

elif page == "Run History":
    st.title("Run History")
    runs = database.list_runs(100)

    if not runs:
        st.info("No sourcing runs have been recorded yet.")
    else:
        import pandas as pd
        df = pd.DataFrame(runs)
        display_cols = [
            "id", "started_at", "user_email", "target",
            "accepted_count", "duplicate_count",
            "rejected_count", "partner_error_count", "status"
        ]
        st.dataframe(df[display_cols], use_container_width=True)

        run_id = st.number_input("Open run ID", min_value=1, step=1, value=int(runs[0]["id"]))
        selected = database.get_run(int(run_id))
        if selected:
            report = json.loads(selected["report_json"])
            st.json(report)

elif page == "Admin":
    if user["role"] != "admin":
        st.error("Admin access required.")
        st.stop()

    st.title("Team Administration")
    st.caption("Create additional team accounts.")

    with st.form("new_user"):
        email = st.text_input("Team member email")
        password = st.text_input("Temporary password", type="password")
        role = st.selectbox("Role", ["user", "admin"])
        submitted = st.form_submit_button("Create user")
        if submitted:
            try:
                database.create_user(email, password, role)
                st.success(f"Created {email}.")
            except Exception as exc:
                st.error(f"Could not create user: {exc}")

    st.markdown("### Existing users")
    st.dataframe(database.list_users(), use_container_width=True)
