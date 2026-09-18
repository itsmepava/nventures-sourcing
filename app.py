from dotenv import load_dotenv
from pathlib import Path
from datetime import datetime, timezone
import json
import os
import time

import streamlit as st

import database
from config import settings
from google_sheets import get_worksheets
from sourcing_engine import (
    run_sourcing,
    discover_partners,
    add_discovered_partners_to_sheet,
    run_sri_lankan_founder_sourcing,
)

# ============================================================================
# APP SETUP
# ============================================================================

BASE_DIR = Path(__file__).resolve().parent
load_dotenv(BASE_DIR / ".env", override=True)

st.set_page_config(
    page_title="nVentures Sourcing",
    page_icon="🚀",
    layout="wide",
    initial_sidebar_state="expanded",
)

database.init_db()

# Bootstrap the first admin only if the account does not already exist.
admin_email = os.getenv("ADMIN_EMAIL", "").strip().lower()
admin_password = os.getenv("ADMIN_PASSWORD", "")

if admin_email and admin_password:
    database.ensure_admin(admin_email, admin_password)

LOGO_PATH = BASE_DIR / "assets" / "nventures_logo.png"

# ============================================================================
# DARK nVENTURES UI
# ============================================================================

st.markdown(
    """
    <style>
    :root {
        --nv-bg: #080808;
        --nv-panel: #111111;
        --nv-panel-2: #151515;
        --nv-border: #292929;
        --nv-text: #f5f7fa;
        --nv-muted: #9ca3af;
        --nv-blue: #0b74ff;
        --nv-blue-dark: #075dcc;
    }

    .stApp,
    [data-testid="stAppViewContainer"],
    .main {
        background: var(--nv-bg);
        color: var(--nv-text);
    }

    [data-testid="stHeader"] {
        background: transparent;
    }

    #MainMenu,
    footer {
        visibility: hidden;
    }

    /* Sidebar */
    section[data-testid="stSidebar"] {
        background: #050505;
        border-right: 1px solid #202020;
    }

    section[data-testid="stSidebar"] > div {
        background: #050505;
    }

    section[data-testid="stSidebar"] label,
    section[data-testid="stSidebar"] p,
    section[data-testid="stSidebar"] span,
    section[data-testid="stSidebar"] small {
        color: var(--nv-text);
    }

    /* Typography */
    h1, h2, h3, h4, h5, h6,
    .stMarkdown, .stCaption,
    p, label {
        color: var(--nv-text);
    }

    .stCaption {
        color: var(--nv-muted) !important;
    }

    /* Hero */
    .nv-hero {
        background: #050505;
        border: 1px solid #262626;
        border-radius: 16px;
        padding: 30px 32px;
        margin-bottom: 22px;
        box-shadow: 0 12px 35px rgba(0,0,0,.22);
    }

    .nv-hero-title {
        color: #ffffff;
        font-size: 38px;
        line-height: 1.05;
        font-weight: 800;
        letter-spacing: -0.03em;
    }

    .nv-hero-subtitle {
        color: #aeb6c2;
        font-size: 15px;
        margin-top: 10px;
    }

    /* Cards */
    .nv-card {
        background: var(--nv-panel);
        border: 1px solid var(--nv-border);
        border-radius: 13px;
        padding: 18px 20px;
        margin-bottom: 14px;
    }

    .nv-card-title {
        color: #ffffff;
        font-size: 14px;
        font-weight: 750;
        margin-bottom: 7px;
    }

    .nv-card-value {
        color: #ffffff;
        font-size: 19px;
        font-weight: 750;
    }

    .nv-card-text {
        color: #aeb6c2;
        font-size: 13px;
        line-height: 1.55;
    }

    .nv-blue-card {
        background: #0d1824;
        border: 1px solid #194d80;
        border-radius: 13px;
        padding: 18px 20px;
        margin-bottom: 18px;
    }

    .nv-blue-title {
        color: #69adff;
        font-size: 14px;
        font-weight: 750;
    }

    .nv-blue-value {
        color: #ffffff;
        font-size: 20px;
        font-weight: 800;
        margin-top: 5px;
    }

    .nv-blue-text {
        color: #b7c2d0;
        font-size: 13px;
        line-height: 1.5;
        margin-top: 6px;
    }

    /* Metrics */
    div[data-testid="stMetric"] {
        background: var(--nv-panel);
        border: 1px solid var(--nv-border);
        border-radius: 12px;
        padding: 17px;
    }

    div[data-testid="stMetric"] label {
        color: #9ca3af !important;
    }

    div[data-testid="stMetric"] [data-testid="stMetricValue"] {
        color: #ffffff !important;
    }

    /* Inputs */
    div[data-baseweb="input"] > div,
    div[data-baseweb="textarea"] > div,
    div[data-baseweb="select"] > div {
        background: var(--nv-panel-2);
        border-color: #353535;
        color: #ffffff;
    }

    input,
    textarea {
        color: #ffffff !important;
        caret-color: #ffffff;
    }

    /* Buttons */
    .stButton > button,
    .stFormSubmitButton > button {
        background: #151515;
        color: #ffffff;
        border: 1px solid #3a3a3a;
        border-radius: 8px;
        font-weight: 650;
        min-height: 42px;
    }

    .stButton > button:hover,
    .stFormSubmitButton > button:hover {
        border-color: var(--nv-blue);
        color: #ffffff;
    }

    .stButton > button[kind="primary"],
    .stFormSubmitButton > button[kind="primary"] {
        background: var(--nv-blue);
        border-color: var(--nv-blue);
        color: #ffffff;
        font-weight: 750;
        min-height: 48px;
    }

    .stButton > button[kind="primary"]:hover,
    .stFormSubmitButton > button[kind="primary"]:hover {
        background: var(--nv-blue-dark);
        border-color: var(--nv-blue-dark);
    }

    /* Alerts */
    div[data-testid="stAlert"] {
        background: #121212;
        border: 1px solid #303030;
        color: #f5f7fa;
    }

    div[data-testid="stAlert"] p {
        color: #f5f7fa !important;
    }

    /* Expanders */
    div[data-testid="stExpander"] {
        background: #111111;
        border: 1px solid #292929;
        border-radius: 10px;
    }

    div[data-testid="stExpander"] summary {
        color: #ffffff !important;
    }

    /* Dataframe */
    div[data-testid="stDataFrame"] {
        background: #111111;
        border: 1px solid #292929;
        border-radius: 10px;
    }

    hr {
        border-color: #292929 !important;
    }

    /* Credit */
    .nv-credit {
        margin-top: 26px;
        padding: 14px;
        border: 1px solid #303030;
        border-radius: 10px;
        background: #0b0b0b;
        text-align: center;
    }

    .nv-credit-small {
        font-size: 12px;
        color: #999999;
        margin-bottom: 5px;
    }

    .nv-credit-name {
        font-size: 14px;
        font-weight: 750;
        color: #ffffff;
    }

    .nv-credit-product {
        font-size: 11px;
        color: #777777;
        margin-top: 5px;
    }

    /* Login */
    .nv-login {
        max-width: 560px;
        margin: 70px auto 0 auto;
        background: #0d0d0d;
        border: 1px solid #282828;
        border-radius: 16px;
        padding: 30px;
        box-shadow: 0 18px 55px rgba(0,0,0,.30);
    }

    .nv-login-title {
        color: #ffffff;
        text-align: center;
        font-size: 31px;
        font-weight: 800;
        letter-spacing: -0.025em;
        margin-top: 18px;
    }

    .nv-login-subtitle {
        color: #9ca3af;
        text-align: center;
        font-size: 14px;
        margin-top: 6px;
        margin-bottom: 24px;
    }
    </style>
    """,
    unsafe_allow_html=True,
)

# ============================================================================
# LOGIN
# ============================================================================

if "user" not in st.session_state:
    st.session_state.user = None

if not st.session_state.user:
    st.markdown('<div class="nv-login">', unsafe_allow_html=True)

    if LOGO_PATH.exists():
        st.image(str(LOGO_PATH), use_container_width=True)
    else:
        st.markdown(
            '<div style="text-align:center;color:white;font-size:36px;font-weight:800;">nVentures</div>',
            unsafe_allow_html=True,
        )

    st.markdown(
        '<div class="nv-login-title">Sourcing Intelligence</div>',
        unsafe_allow_html=True,
    )
    st.markdown(
        '<div class="nv-login-subtitle">Private sourcing platform for the nVentures team.</div>',
        unsafe_allow_html=True,
    )

    with st.form("login_form"):
        email = st.text_input("Email", placeholder="you@company.com")
        password = st.text_input("Password", type="password")
        submitted = st.form_submit_button(
            "Sign in",
            type="primary",
            use_container_width=True,
        )

    if submitted:
        user = database.authenticate(email.strip().lower(), password)
        if user:
            st.session_state.user = user
            st.rerun()
        else:
            st.error("Invalid email or password.")

    st.markdown("</div>", unsafe_allow_html=True)
    st.stop()

user = st.session_state.user

if "partner_discovery_results" not in st.session_state:
    st.session_state.partner_discovery_results = []

# ============================================================================
# SIDEBAR
# ============================================================================

if LOGO_PATH.exists():
    st.sidebar.image(str(LOGO_PATH), use_container_width=True)
else:
    st.sidebar.markdown("# nVentures")

st.sidebar.divider()
st.sidebar.caption(f"Signed in as {user['email']}")
st.sidebar.caption(f"Role: {user['role']}")

if st.sidebar.button("Sign out", use_container_width=True):
    st.session_state.user = None
    st.rerun()

st.sidebar.divider()

pages = ["Dashboard", "Sri Lankan Founder Sourcing", "Partner Discovery", "Run History"]
if user["role"] == "admin":
    pages.append("Admin")

page = st.sidebar.radio("Navigate", pages)

st.sidebar.markdown(
    """
    <div class="nv-credit">
        <div class="nv-credit-small">Built by</div>
        <div class="nv-credit-name">Pavara Kekulawala</div>
        <div class="nv-credit-product">nVentures Sourcing Platform</div>
    </div>
    """,
    unsafe_allow_html=True,
)

# ============================================================================
# DASHBOARD
# ============================================================================

if page == "Dashboard":
    st.markdown(
        """
        <div class="nv-hero">
            <div class="nv-hero-title">Sourcing Intelligence</div>
            <div class="nv-hero-subtitle">
                AI-powered company discovery, research and investment sourcing.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    st.markdown("### Sourcing controls")

    c1, c2, c3 = st.columns(3)

    with c1:
        target = st.number_input(
            "Target new companies",
            min_value=1,
            max_value=100,
            value=25,
            step=1,
        )

    with c2:
        max_partners = st.number_input(
            "Partners per run",
            min_value=1,
            max_value=50,
            value=int(settings.max_partners),
            step=1,
        )

    with c3:
        max_research = st.number_input(
            "Deep research limit",
            min_value=1,
            max_value=200,
            value=int(settings.max_deep_research),
            step=1,
        )

    st.markdown("### Investment criteria")

    st.markdown(
        f"""
        <div class="nv-card">
            <div class="nv-card-title">Current screening mandate</div>
            <div class="nv-card-text">
                <b>B2B:</b> required &nbsp; • &nbsp;
                <b>Geography:</b> no restriction &nbsp; • &nbsp;
                <b>Funding:</b> no ceiling / stage restriction
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    st.markdown("### System status")

    openrouter_ready = bool(os.getenv("OPENROUTER_API_KEY", "").strip())
    google_ready = bool(os.getenv("GOOGLE_SERVICE_ACCOUNT_JSON", "").strip())

    s1, s2, s3 = st.columns(3)

    with s1:
        if openrouter_ready:
            st.success("OpenRouter configured")
        else:
            st.error("OpenRouter key missing")

    with s2:
        st.success("FreeSerp web search configured")

    with s3:
        if google_ready:
            st.success("Google Sheets configured")
        else:
            st.error("Google service account missing")

    st.divider()

    if st.button(
        "🚀 Start sourcing",
        type="primary",
        use_container_width=True,
    ):
        started = datetime.now(timezone.utc).isoformat()

        progress = st.progress(0)
        status = st.empty()

        st.markdown("### Live log")

        log_placeholder = st.empty()
        log_buffer = []
        last_render = [0.0]

        # Streamlit repaints the whole element on every update, so redraw at
        # most a few times a second. Bursty output (API retries) would
        # otherwise spend more time rendering than sourcing.
        LOG_VISIBLE_LINES = 300
        LOG_MIN_REDRAW_SECONDS = 0.3

        def render_log(force=False):
            now = time.monotonic()
            if not force and now - last_render[0] < LOG_MIN_REDRAW_SECONDS:
                return
            last_render[0] = now
            log_placeholder.code(
                "\n".join(log_buffer[-LOG_VISIBLE_LINES:]) or "Waiting...",
                language="log",
            )

        def on_log(line):
            log_buffer.append(line)
            render_log()

        render_log(force=True)

        try:
            status.info("Connecting to Google Sheets...")

            (
                _sh,
                sourcing_ws,
                partner_ws,
                control_ws,
                _partner_name,
            ) = get_worksheets(
                settings.spreadsheet_id,
                settings.sourcing_tab,
                settings.control_tab,
                settings.partner_tab_candidates,
            )

            status.success("Google Sheets connected.")

            def on_progress(value, message=""):
                progress.progress(max(0, min(100, int(value))))
                if message:
                    status.write(message)

            status.info(
                "Sourcing engine is running. This can take several minutes "
                "because companies are researched and verified individually."
            )

            report = run_sourcing(
                sourcing_ws=sourcing_ws,
                partner_ws=partner_ws,
                control_ws=control_ws,
                openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
                tavily_api_key=os.getenv("TAVILY_API_KEY", ""),
                openrouter_model=settings.openrouter_model,
                target_companies=int(target),
                max_partners=int(max_partners),
                max_deep_research=int(max_research),
                max_candidates_per_partner=settings.max_candidates_per_partner,
                max_total_funding=settings.max_total_funding,
                max_team_size_warning=settings.max_team_size_warning,
                tavily_timeout=settings.freeserp_timeout,
                openrouter_timeout=settings.openrouter_timeout,
                max_tavily_results=settings.max_freeserp_results,
                max_research_chars=settings.max_research_chars,
                request_delay=settings.request_delay,
                progress_callback=on_progress,
                log_callback=on_log,
            )

            finished = datetime.now(timezone.utc).isoformat()

            # Normalize the report keys defensively so run history/database
            # counts remain correct even if the engine wrapper changes.
            report.setdefault("accepted", [])
            report.setdefault("rejected", [])
            report.setdefault("duplicates", [])
            report.setdefault("partner_errors", [])
            report.setdefault("accepted_details", [])

            run_id = database.save_run(
                user["email"],
                started,
                finished,
                int(target),
                report,
            )

            progress.progress(100)
            render_log(force=True)
            status.success(f"Run #{run_id} completed.")

            st.download_button(
                "Download run log",
                "\n".join(log_buffer),
                file_name=f"sourcing_run_{run_id}.log",
                mime="text/plain",
            )

            accepted = report["accepted"]
            rejected = report["rejected"]
            duplicates = report["duplicates"]
            errors = report["partner_errors"]

            m1, m2, m3, m4 = st.columns(4)
            m1.metric("Added", len(accepted))
            m2.metric("Duplicates", len(duplicates))
            m3.metric("Rejected", len(rejected))
            m4.metric("Partner errors", len(errors))

            if accepted:
                st.markdown("### New companies")
                st.dataframe(
                    report["accepted_details"] or accepted,
                    use_container_width=True,
                )
            else:
                st.info("No new companies were accepted during this run.")

            if rejected:
                with st.expander(f"Rejected ({len(rejected)})"):
                    st.write(rejected)

            if duplicates:
                with st.expander(f"Duplicates ({len(duplicates)})"):
                    st.write(duplicates)

            if errors:
                with st.expander(f"Partner/API errors ({len(errors)})"):
                    st.write(errors)

            with st.expander("Full run report"):
                # The log is already on screen and can be megabytes; showing it
                # again inside st.json makes the widget crawl.
                st.json({k: v for k, v in report.items() if k != "log"})

        except Exception as exc:
            render_log(force=True)
            st.error("The sourcing run failed.")
            st.exception(exc)

            if log_buffer:
                st.download_button(
                    "Download partial run log",
                    "\n".join(log_buffer),
                    file_name="sourcing_run_failed.log",
                    mime="text/plain",
                )


# ============================================================================
# SRI LANKAN FOUNDER SOURCING
# ============================================================================

elif page == "Sri Lankan Founder Sourcing":
    st.markdown(
        """
        <div class="nv-hero">
            <div class="nv-hero-title">Sri Lankan Founder Sourcing</div>
            <div class="nv-hero-subtitle">
                Find globally based companies with verified Sri Lankan
                founders or co-founders.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    st.markdown("### Sourcing mandate")

    st.markdown(
        """
        <div class="nv-card">
            <div class="nv-card-title">Founder-based sourcing</div>
            <div class="nv-card-text">
                <b>Founder:</b> at least one founder/co-founder must be
                verifiably Sri Lankan &nbsp; • &nbsp;
                <b>Company geography:</b> no restriction &nbsp; • &nbsp;
                <b>B2B:</b> required
                <br><br>
                The system does not infer nationality from a name, surname,
                location or other indirect signals. A public evidence trail is
                required before a company is added.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    c1, c2, c3 = st.columns(3)

    with c1:
        founder_target = st.number_input(
            "Target new companies",
            min_value=1,
            max_value=100,
            value=25,
            step=1,
            key="sl_founder_target",
        )

    with c2:
        founder_research = st.number_input(
            "Deep research limit",
            min_value=1,
            max_value=200,
            value=int(settings.max_deep_research),
            step=1,
            key="sl_founder_research",
        )

    with c3:
        founder_partners = st.number_input(
            "VC partners to scan",
            min_value=1,
            max_value=100,
            value=12,
            step=1,
            key="sl_founder_partners",
            help="Maximum number of VC/investor partners from Partner Database to scan for portfolio companies.",
        )

    st.caption(
        "Companies are written into the existing Active Sourcing sheet. "
        "Founder evidence is preserved where matching founder/evidence "
        "columns exist, and otherwise in Extra Notes. The run combines "
        "Web Discovery with VC Portfolio scanning and deduplicates before "
        "deep research/write."
    )

    openrouter_ready = bool(os.getenv("OPENROUTER_API_KEY", "").strip())
    google_ready = bool(os.getenv("GOOGLE_SERVICE_ACCOUNT_JSON", "").strip())

    s1, s2, s3 = st.columns(3)

    with s1:
        if openrouter_ready:
            st.success("OpenRouter configured")
        else:
            st.error("OpenRouter key missing")

    with s2:
        st.success("FreeSerp web search configured")

    with s3:
        if google_ready:
            st.success("Google Sheets configured")
        else:
            st.error("Google service account missing")

    st.divider()

    if st.button(
        "🇱🇰 Start Sri Lankan Founder Sourcing",
        type="primary",
        use_container_width=True,
        disabled=not (openrouter_ready and google_ready),
    ):
        started = datetime.now(timezone.utc).isoformat()

        progress = st.progress(0)
        status = st.empty()
        st.markdown("### Live log")

        log_placeholder = st.empty()
        log_buffer = []
        last_render = [0.0]

        LOG_VISIBLE_LINES = 300
        LOG_MIN_REDRAW_SECONDS = 0.3

        def render_founder_log(force=False):
            now = time.monotonic()
            if not force and now - last_render[0] < LOG_MIN_REDRAW_SECONDS:
                return
            last_render[0] = now
            log_placeholder.code(
                "\n".join(log_buffer[-LOG_VISIBLE_LINES:]) or "Waiting...",
                language="log",
            )

        def on_founder_log(line):
            log_buffer.append(line)
            render_founder_log()

        render_founder_log(force=True)

        try:
            status.info("Connecting to Google Sheets...")

            (
                _sh,
                sourcing_ws,
                _partner_ws,
                _control_ws,
                _partner_name,
            ) = get_worksheets(
                settings.spreadsheet_id,
                settings.sourcing_tab,
                settings.control_tab,
                settings.partner_tab_candidates,
            )

            status.success("Google Sheets connected.")

            def on_founder_progress(value, message=""):
                progress.progress(max(0, min(100, int(value))))
                if message:
                    status.info(message)

            status.info(
                "Searching the web + VC portfolios for companies with verified Sri Lankan founders..."
            )

            report = run_sri_lankan_founder_sourcing(
                sourcing_ws=sourcing_ws,
                partner_ws=_partner_ws,
                control_ws=_control_ws,
                openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
                tavily_api_key=os.getenv("TAVILY_API_KEY", ""),
                openrouter_model=settings.openrouter_model,
                target_companies=int(founder_target),
                max_partners=int(founder_partners),
                max_deep_research=int(founder_research),
                max_candidates_per_search=8,
                max_candidates_per_partner=10,
                tavily_timeout=settings.freeserp_timeout,
                openrouter_timeout=settings.openrouter_timeout,
                max_tavily_results=settings.max_freeserp_results,
                max_research_chars=settings.max_research_chars,
                request_delay=settings.request_delay,
                progress_callback=on_founder_progress,
                log_callback=on_founder_log,
            )

            report.setdefault("accepted", [])
            report.setdefault("rejected", [])
            report.setdefault("duplicates", [])
            report.setdefault("partner_errors", [])
            report.setdefault("accepted_details", [])

            finished = datetime.now(timezone.utc).isoformat()

            run_id = database.save_run(
                user["email"],
                started,
                finished,
                int(founder_target),
                report,
            )

            progress.progress(100)
            render_founder_log(force=True)
            status.success(f"Run #{run_id} completed.")

            st.download_button(
                "Download run log",
                "\n".join(log_buffer),
                file_name=f"sri_lankan_founder_run_{run_id}.log",
                mime="text/plain",
            )

            accepted = report["accepted"]
            rejected = report["rejected"]
            duplicates = report["duplicates"]
            errors = report["partner_errors"]

            m1, m2, m3, m4 = st.columns(4)
            m1.metric("Added", len(accepted))
            m2.metric("Duplicates", len(duplicates))
            m3.metric("Rejected", len(rejected))
            m4.metric("Research/API errors", len(errors))

            if accepted:
                st.markdown("### New companies")

                display_rows = [
                    {
                        "Company": item.get("company", ""),
                        "Founder": item.get("founder", ""),
                        "Founder evidence": item.get("founder_evidence", ""),
                        "Company HQ": item.get("headquarters", ""),
                        "Sector": item.get("sector", ""),
                        "Evidence URL": item.get("evidence_url", ""),
                        "Source": item.get("source", ""),
                        "Partner VC(s)": ", ".join(item.get("partner_vcs", []) or []),
                        "Sheet row": item.get("row", ""),
                    }
                    for item in report["accepted_details"]
                ]

                st.dataframe(
                    display_rows,
                    use_container_width=True,
                    hide_index=True,
                )
            else:
                st.info("No new Sri Lankan-founder companies were accepted.")

            if rejected:
                with st.expander(f"Rejected ({len(rejected)})"):
                    st.write(rejected)

            if duplicates:
                with st.expander(f"Duplicates ({len(duplicates)})"):
                    st.write(duplicates)

            if errors:
                with st.expander(f"Research/API errors ({len(errors)})"):
                    st.write(errors)

            with st.expander("Full run report"):
                st.json({k: v for k, v in report.items() if k != "log"})

        except Exception as exc:
            render_founder_log(force=True)
            st.error("The Sri Lankan founder sourcing run failed.")
            st.exception(exc)

            if log_buffer:
                st.download_button(
                    "Download partial run log",
                    "\n".join(log_buffer),
                    file_name="sri_lankan_founder_run_failed.log",
                    mime="text/plain",
                )


# ============================================================================
# PARTNER DISCOVERY
# ============================================================================

elif page == "Partner Discovery":
    st.markdown(
        """
        <div class="nv-hero">
            <div class="nv-hero-title">Partner Discovery</div>
            <div class="nv-hero-subtitle">
                Find South Asia-based VCs, accelerators, angel networks,
                incubators and other potential co-investment partners.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    st.markdown("### Discovery controls")

    d1, d2 = st.columns(2)

    with d1:
        discovery_countries = st.multiselect(
            "Countries",
            [
                "India",
                "Sri Lanka",
                "Bangladesh",
                "Pakistan",
                "Nepal",
                "Bhutan",
                "Maldives",
                "Afghanistan",
                "Singapore",
                "Vietnam",
                "Indonesia"
            ],
            default=[
                "India",
                "Sri Lanka",
                "Bangladesh",
                "Pakistan",
                "Nepal",
                "Bhutan",
                "Maldives",
                "Afghanistan",
                "Singapore",
                "Vietnam",
                "Indonesia"
            ],
        )

    with d2:
        discovery_types = st.multiselect(
            "Partner types",
            [
                "Venture Capital",
                "Accelerator",
                "Angel Syndicate / Angel Network",
                "Incubator",
                "Seed Fund",
                "Corporate Venture Capital",
                "Family Office",
            ],
            default=[
                "Venture Capital",
                "Accelerator",
                "Angel Syndicate / Angel Network",
                "Incubator",
                "Seed Fund",
            ],
        )

    st.caption(
        "The tool researches organizations that are based in the selected "
        "countries. Results are suggestions for the nVentures partner database "
        "and should be verified before outreach."
    )

    openrouter_ready = bool(os.getenv("OPENROUTER_API_KEY", "").strip())
    google_ready = bool(os.getenv("GOOGLE_SERVICE_ACCOUNT_JSON", "").strip())

    if not (openrouter_ready and google_ready):
        st.warning(
            "OpenRouter and Google Sheets must be configured "
            "before partner discovery can run."
        )

    if st.button(
        "🔎 Find Partners",
        type="primary",
        use_container_width=True,
        disabled=not (openrouter_ready and google_ready),
    ):
        progress = st.progress(0)
        status = st.empty()
        log_placeholder = st.empty()
        log_buffer = []
        last_render = [0.0]

        def render_discovery_log(force=False):
            now = time.monotonic()
            if not force and now - last_render[0] < 0.3:
                return
            last_render[0] = now
            log_placeholder.code(
                "\n".join(log_buffer[-200:]) or "Waiting...",
                language="log",
            )

        def discovery_log(line):
            log_buffer.append(line)
            render_discovery_log()

        def discovery_progress(value, message=""):
            progress.progress(max(0, min(100, int(value))))
            if message:
                status.info(message)

        try:
            status.info("Connecting to Google Sheets...")
            (
                _sh,
                _sourcing_ws,
                partner_ws,
                _control_ws,
                _partner_name,
            ) = get_worksheets(
                settings.spreadsheet_id,
                settings.sourcing_tab,
                settings.control_tab,
                settings.partner_tab_candidates,
            )

            status.info("Researching potential partners...")
            result = discover_partners(
                openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
                tavily_api_key=os.getenv("TAVILY_API_KEY", ""),
                openrouter_model=settings.openrouter_model,
                countries=discovery_countries,
                partner_types=discovery_types,
                tavily_timeout=settings.freeserp_timeout,
                openrouter_timeout=settings.openrouter_timeout,
                max_tavily_results=8,
                max_research_chars=12000,
                request_delay=0.5,
                progress_callback=discovery_progress,
                log_callback=discovery_log,
            )

            st.session_state.partner_discovery_results = result["partners"]
            render_discovery_log(force=True)
            status.success(
                f"Discovery complete — {len(result['partners'])} unique candidates found."
            )

        except Exception as exc:
            render_discovery_log(force=True)
            st.error("Partner discovery failed.")
            st.exception(exc)

    results = st.session_state.get("partner_discovery_results", [])

    if results:
        st.divider()
        st.markdown("### Discovered partners")

        import pandas as pd

        display_rows = [
            {
                "Partner": r.get("name", ""),
                "Type": r.get("type", ""),
                "Country": r.get("country", ""),
                "City": r.get("city", ""),
                "Investment Focus": r.get("investment_focus", ""),
                "Stage": r.get("stage_focus", ""),
                "Typical Check": r.get("typical_check", ""),
                "Confidence": r.get("confidence", ""),
            }
            for r in results
        ]

        st.dataframe(
            pd.DataFrame(display_rows),
            use_container_width=True,
            hide_index=True,
        )

        selected_names = st.multiselect(
            "Select partners to add to the Partner sheet",
            [r.get("name", "") for r in results if r.get("name")],
        )

        if selected_names and st.button(
            "➕ Add selected partners to Google Sheets",
            type="primary",
        ):
            try:
                (
                    _sh,
                    _sourcing_ws,
                    partner_ws,
                    _control_ws,
                    _partner_name,
                ) = get_worksheets(
                    settings.spreadsheet_id,
                    settings.sourcing_tab,
                    settings.control_tab,
                    settings.partner_tab_candidates,
                )

                selected_records = [
                    r for r in results if r.get("name") in selected_names
                ]

                add_result = add_discovered_partners_to_sheet(
                    partner_ws,
                    selected_records,
                    log=print,
                )

                if add_result["added"]:
                    st.success(
                        f"Added {len(add_result['added'])} partner(s) to Google Sheets."
                    )

                if add_result["skipped"]:
                    st.info(
                        "Skipped existing partners: "
                        + ", ".join(add_result["skipped"])
                    )

                # Remove successfully added records from the pending list.
                added_set = set(add_result["added"])
                st.session_state.partner_discovery_results = [
                    r for r in results if r.get("name") not in added_set
                ]
                st.rerun()

            except Exception as exc:
                st.error("Could not add partners to Google Sheets.")
                st.exception(exc)

        st.markdown("### Partner details")

        for record in results:
            with st.expander(
                f"{record.get('name', 'Unknown')} · "
                f"{record.get('type', 'Unknown')} · "
                f"{record.get('country', 'Unknown')}"
            ):
                c1, c2 = st.columns(2)

                with c1:
                    st.write(f"**Website:** {record.get('website', '') or '—'}")
                    st.write(f"**LinkedIn:** {record.get('linkedin', '') or '—'}")
                    st.write(
                        f"**Investment focus:** "
                        f"{record.get('investment_focus', '') or '—'}"
                    )
                    st.write(
                        f"**Stage focus:** "
                        f"{record.get('stage_focus', '') or '—'}"
                    )

                with c2:
                    st.write(
                        f"**Typical check:** "
                        f"{record.get('typical_check', '') or '—'}"
                    )
                    st.write(
                        f"**Portfolio examples:** "
                        f"{record.get('portfolio_examples', '') or '—'}"
                    )
                    st.write(
                        f"**Recent activity:** "
                        f"{record.get('recent_activity', '') or '—'}"
                    )
                    st.write(
                        f"**Why relevant:** "
                        f"{record.get('reason_relevant', '') or '—'}"
                    )

# ============================================================================
# RUN HISTORY
# ============================================================================

elif page == "Run History":
    st.markdown(
        """
        <div class="nv-hero">
            <div class="nv-hero-title">Run History</div>
            <div class="nv-hero-subtitle">
                Previous sourcing runs performed by the nVentures team.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    runs = database.list_runs(100)

    if not runs:
        st.info("No sourcing runs have been recorded yet.")
    else:
        import pandas as pd

        df = pd.DataFrame(runs)

        preferred = [
            "id",
            "started_at",
            "finished_at",
            "user_email",
            "target",
            "accepted_count",
            "duplicate_count",
            "rejected_count",
            "partner_error_count",
            "status",
        ]

        display_cols = [c for c in preferred if c in df.columns]

        st.dataframe(
            df[display_cols],
            use_container_width=True,
            hide_index=True,
        )

        st.markdown("### Open run")

        run_ids = [int(r["id"]) for r in runs]
        selected_id = st.selectbox("Select a run", run_ids)

        selected = database.get_run(selected_id)

        if selected:
            st.markdown(
                f"""
                <div class="nv-card">
                    <div class="nv-card-title">Run #{selected['id']}</div>
                    <div class="nv-card-text">
                        <b>User:</b> {selected['user_email']}<br>
                        <b>Started:</b> {selected['started_at']}<br>
                        <b>Finished:</b> {selected['finished_at']}<br>
                        <b>Status:</b> {selected['status']}
                    </div>
                </div>
                """,
                unsafe_allow_html=True,
            )

            try:
                st.json(json.loads(selected["report_json"]))
            except Exception:
                st.code(selected.get("report_json", ""))

# ============================================================================
# ADMIN
# ============================================================================

elif page == "Admin":
    if user["role"] != "admin":
        st.error("Admin access required.")
        st.stop()

    st.markdown(
        """
        <div class="nv-hero">
            <div class="nv-hero-title">Team Administration</div>
            <div class="nv-hero-subtitle">
                Create and manage accounts for the sourcing team.
            </div>
        </div>
        """,
        unsafe_allow_html=True,
    )

    st.markdown("### Create team member")

    with st.form("new_user_form"):
        new_email = st.text_input("Team member email")
        new_password = st.text_input("Temporary password", type="password")
        new_role = st.selectbox("Role", ["user", "admin"])

        submitted = st.form_submit_button(
            "Create user",
            type="primary",
        )

        if submitted:
            email_clean = new_email.strip().lower()

            if not email_clean:
                st.error("Email is required.")
            elif not new_password:
                st.error("Password is required.")
            else:
                try:
                    database.create_user(
                        email_clean,
                        new_password,
                        new_role,
                    )
                    st.success(f"Created {email_clean}.")
                except Exception as exc:
                    st.error(f"Could not create user: {exc}")

    st.markdown("### Existing users")

    users = database.list_users()

    if users:
        st.dataframe(
            users,
            use_container_width=True,
            hide_index=True,
        )
    else:
        st.info("No users found.")

    st.markdown("### Reset a password")

    with st.form("reset_password_form"):
        reset_email = st.text_input("Account email")
        reset_password = st.text_input("New password", type="password")

        reset_submitted = st.form_submit_button("Reset password")

        if reset_submitted:
            email_clean = reset_email.strip().lower()

            if not email_clean or not reset_password:
                st.error("Email and new password are required.")
            elif database.reset_password(email_clean, reset_password):
                st.success("Password reset successfully.")
            else:
                st.error("No active account exists with that email.")
