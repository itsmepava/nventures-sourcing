import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    spreadsheet_id: str = os.getenv(
        "SPREADSHEET_ID",
        "1GQ3dADRqopII1Mg2O3MjqWJ3tFyWI_sre0urgeT5nNs",
    )

    sourcing_tab: str = os.getenv(
        "SOURCING_TAB_NAME",
        "Active Sourcing",
    )

    control_tab: str = os.getenv(
        "CONTROL_TAB_NAME",
        "Sourcing Control",
    )

    partner_tab_candidates: tuple = (
        "Partner List",
        "Partner",
        "Partners",
        "Partner tab",
    )

    # OpenRouter
    openrouter_model: str = os.getenv(
        "OPENROUTER_MODEL",
        "minimax/minimax-m3",
    )

    openrouter_timeout: int = int(
        os.getenv("OPENROUTER_TIMEOUT", "120")
    )

    # Sourcing limits
    max_partners: int = int(
        os.getenv("MAX_PARTNERS_PER_RUN", "12")
    )

    max_candidates_per_partner: int = int(
        os.getenv("MAX_CANDIDATES_PER_PARTNER", "10")
    )

    max_deep_research: int = int(
        os.getenv("MAX_DEEP_RESEARCH_PER_RUN", "45")
    )

    max_research_chars: int = int(
        os.getenv("MAX_RESEARCH_CHARS", "14000")
    )

    request_delay: float = float(
        os.getenv("REQUEST_DELAY_SECONDS", "1.0")
    )

    # FreeSerp
    freeserp_timeout: int = int(
        os.getenv("FREESERP_TIMEOUT", "120")
    )

    max_freeserp_results: int = int(
        os.getenv("MAX_FREESERP_RESULTS", "10")
    )


settings = Settings()
