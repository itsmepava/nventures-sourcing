import os
from dataclasses import dataclass

# ---------------------------------------------------------------------------
# OPENROUTER: FREE-ONLY SAFETY
# ---------------------------------------------------------------------------
# Never allow an environment variable / Streamlit secret to silently select
# a paid OpenRouter model. OpenRouter's free router always selects from the
# currently available free models.
FREE_OPENROUTER_MODEL = "openrouter/free"


def _get_free_openrouter_model():
    requested = os.getenv("OPENROUTER_MODEL", "").strip()

    # Explicitly permitted free slugs. Anything else, including a paid model
    # such as minimax/minimax-m3, is ignored and replaced with the free router.
    allowed_free_models = {
        "openrouter/free",
        "minimax/minimax-m3:free",
    }

    if requested in allowed_free_models:
        return requested

    return FREE_OPENROUTER_MODEL


@dataclass(frozen=True)
class Settings:
    spreadsheet_id: str = os.getenv("SPREADSHEET_ID", "1GQ3dADRqopII1Mg2O3MjqWJ3tFyWI_sre0urgeT5nNs")
    sourcing_tab: str = os.getenv("SOURCING_TAB_NAME", "Active Sourcing")
    control_tab: str = os.getenv("CONTROL_TAB_NAME", "Sourcing Control")
    partner_tab_candidates: tuple = ("Partner List", "Partner", "Partners", "Partner tab")
    openrouter_model: str = _get_free_openrouter_model()
    max_partners: int = int(os.getenv("MAX_PARTNERS_PER_RUN", "12"))
    max_candidates_per_partner: int = int(os.getenv("MAX_CANDIDATES_PER_PARTNER", "10"))
    max_deep_research: int = int(os.getenv("MAX_DEEP_RESEARCH_PER_RUN", "45"))
    max_total_funding: float = float(os.getenv("MAX_TOTAL_FUNDING_USD", "3000000"))
    max_team_size_warning: int = int(os.getenv("MAX_TEAM_SIZE_WARNING", "30"))
    freeserp_timeout: int = int(os.getenv("FREESERP_TIMEOUT", os.getenv("TAVILY_TIMEOUT", "120")))
    # Backward-compatible alias for older app/engine code.
    tavily_timeout: int = freeserp_timeout
    openrouter_timeout: int = int(os.getenv("OPENROUTER_TIMEOUT", "90"))
    max_freeserp_results: int = int(os.getenv("MAX_FREESERP_RESULTS", os.getenv("MAX_TAVILY_RESULTS", "10")))
    # Backward-compatible alias for older app/engine code.
    max_tavily_results: int = max_freeserp_results
    max_research_chars: int = int(os.getenv("MAX_RESEARCH_CHARS", "14000"))
    request_delay: float = float(os.getenv("REQUEST_DELAY_SECONDS", "1.0"))

settings = Settings()

ELIGIBLE_COUNTRIES = [
    "Afghanistan", "Bangladesh", "Bhutan", "India", "Maldives",
    "Nepal", "Pakistan", "Sri Lanka", "Singapore"
]
