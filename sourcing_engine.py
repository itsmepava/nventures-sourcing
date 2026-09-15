"""
nVentures Sourcing Engine
--------------------------
Sourcing engine for nVentures Fund II.

This module is ordinary importable Python. The previous version stored the
whole engine inside a single-quoted ENGINE_SOURCE string and exec'd it; a
mangled escape sequence in that string broke the module at import time. The
logic below is the same pipeline, written as real functions.

What the engine does:
  - Picks partners from the Partner sheet (least recently sourced first)
  - Extracts portfolio candidates via Tavily + OpenRouter
  - Researches each candidate
  - Enforces B2B / early-stage / funding rules
  - Deduplicates against Google Sheets and within the run
  - Writes accepted companies immediately and reads the row back to verify
  - Continues when an individual partner or API call fails

Geographic filtering has been removed. Country and Headquarters are still
researched and written to the sheet as data, but they no longer gate
acceptance.
"""

import json
import random
import re
import time
from datetime import datetime, timezone

import requests

OPENROUTER_URL = "https://openrouter.ai/api/v1/chat/completions"
TAVILY_URL = "https://api.tavily.com/search"

FALLBACK_MODELS = []


# ============================================================================
# TEXT / HEADER HELPERS
# ============================================================================

def normalize_text(value):
    """Safely convert a value to clean single-spaced text."""
    if value is None:
        return ""

    if isinstance(value, list):
        value = ", ".join(str(x) for x in value if x is not None)
    elif isinstance(value, dict):
        value = json.dumps(value, ensure_ascii=False)

    value = str(value).replace("\xa0", " ")
    return re.sub(r"\s+", " ", value).strip()


def normalize_header(value):
    return re.sub(r"\s+", " ", normalize_text(value).lower()).strip()


def build_header_index(headers):
    """Return (first occurrence map, all occurrences map) for sheet headers."""
    first_idx = {}
    all_idx = {}

    for column_number, header in enumerate(headers, start=1):
        normalized = normalize_header(header)
        if not normalized:
            continue

        all_idx.setdefault(normalized, []).append(column_number)
        if normalized not in first_idx:
            first_idx[normalized] = column_number

    return first_idx, all_idx


def get_col(header, first_idx):
    if not header:
        return None
    return first_idx.get(normalize_header(header))


def worksheet_rows_as_dicts(worksheet):
    values = worksheet.get_all_values()
    if len(values) < 2:
        return []

    headers = values[0]
    rows = []

    for row in values[1:]:
        padded = list(row) + [""] * max(0, len(headers) - len(row))
        rows.append({header: padded[i] for i, header in enumerate(headers)})

    return rows


# ============================================================================
# COMPANY NORMALIZATION / DEDUPLICATION
# ============================================================================

def normalize_company_name(value):
    value = normalize_text(value).lower()
    value = re.sub(
        r"\b(private|pvt|limited|ltd|llp|inc|incorporated|corp|corporation)\b",
        "",
        value,
    )
    return re.sub(r"[^a-z0-9]+", "", value)


def normalize_domain(url):
    url = normalize_text(url).lower()
    if not url:
        return ""

    match = re.search(r"https?://[^)\s]+", url)
    if match:
        url = match.group(0)

    url = re.sub(r"^https?://", "", url)
    url = re.sub(r"^www\.", "", url)
    url = url.split("/")[0].split("?")[0]

    return url.strip()


def normalize_linkedin(url):
    url = normalize_text(url).lower()
    if not url:
        return ""

    match = re.search(r"https?://(?:www\.)?linkedin\.com/[^\s)]+", url)
    if match:
        url = match.group(0)

    return url.rstrip("/")


def build_existing_indexes(worksheet):
    values = worksheet.get_all_values()
    indexes = {"names": set(), "domains": set(), "linkedin": set()}

    if not values:
        return indexes

    headers = values[0]
    name_col = website_col = linkedin_col = None

    for i, header in enumerate(headers):
        normalized = normalize_header(header)
        if normalized == "company name":
            name_col = i
        elif normalized == "website":
            website_col = i
        elif normalized == "company linkedin":
            linkedin_col = i

    for row in values[1:]:
        if name_col is not None and len(row) > name_col:
            name = normalize_company_name(row[name_col])
            if name:
                indexes["names"].add(name)

        if website_col is not None and len(row) > website_col:
            domain = normalize_domain(row[website_col])
            if domain:
                indexes["domains"].add(domain)

        if linkedin_col is not None and len(row) > linkedin_col:
            linkedin = normalize_linkedin(row[linkedin_col])
            if linkedin:
                indexes["linkedin"].add(linkedin)

    return indexes


def company_is_existing(company_name, website="", linkedin="", indexes=None):
    if indexes is None:
        return False

    name = normalize_company_name(company_name)
    domain = normalize_domain(website)
    linkedin = normalize_linkedin(linkedin)

    if name and name in indexes["names"]:
        return True
    if domain and domain in indexes["domains"]:
        return True
    if linkedin and linkedin in indexes["linkedin"]:
        return True

    return False


def add_company_to_indexes(company_name, website="", linkedin="", indexes=None):
    if indexes is None:
        return

    name = normalize_company_name(company_name)
    domain = normalize_domain(website)
    linkedin = normalize_linkedin(linkedin)

    if name:
        indexes["names"].add(name)
    if domain:
        indexes["domains"].add(domain)
    if linkedin:
        indexes["linkedin"].add(linkedin)


# ============================================================================
# HTTP RETRY HELPER
# ============================================================================

def http_request_with_retry(
    method,
    url,
    *,
    headers=None,
    json_body=None,
    timeout=90,
    max_attempts=5,
    label="HTTP request",
    log=print,
):
    last_error = None

    for attempt in range(1, max_attempts + 1):
        try:
            response = requests.request(
                method,
                url,
                headers=headers,
                json=json_body,
                timeout=timeout,
            )

            if response.status_code < 400:
                return response

            if response.status_code in {429, 500, 502, 503, 504}:
                retry_after = response.headers.get("Retry-After")

                if retry_after:
                    try:
                        wait_seconds = float(retry_after)
                    except (TypeError, ValueError):
                        wait_seconds = min(60, 2 ** attempt)
                else:
                    wait_seconds = min(60, 2 ** attempt)

                wait_seconds += random.uniform(0.25, 1.25)

                log(
                    f"      {label}: HTTP {response.status_code}. "
                    f"Retrying in {wait_seconds:.1f}s..."
                )

                last_error = RuntimeError(
                    f"{label} returned HTTP {response.status_code}"
                )

                time.sleep(wait_seconds)
                continue

            raise RuntimeError(
                f"{label} failed with HTTP {response.status_code}:\n"
                f"{response.text[:3000]}"
            )

        except requests.RequestException as error:
            last_error = error
            wait_seconds = min(60, 2 ** attempt) + random.uniform(0.25, 1.25)
            log(f"      {label}: network error. Retrying in {wait_seconds:.1f}s...")
            time.sleep(wait_seconds)

    raise RuntimeError(
        f"{label} failed after {max_attempts} attempts: {last_error}"
    )


# ============================================================================
# OPENROUTER
# ============================================================================

def build_model_order(primary_model):
    """Primary model first, then free fallbacks, de-duplicated in order."""
    models = []
    for model in [normalize_text(primary_model)] + FALLBACK_MODELS:
        if model and model not in models:
            models.append(model)
    return models


DATA_POLICY_HELP = (
    "OpenRouter is refusing every ':free' model for this account with a "
    "data-policy error, not a bad model ID. Free models are subsidized by "
    "letting the provider log/train on the prompt, so OpenRouter requires "
    "'Free model publication' (sometimes shown as 'Enable training and "
    "logging') to be turned on in the account's privacy settings before it "
    "will route to ANY free model - every model in the fallback chain will "
    "fail identically until that's enabled, since it's an account setting, "
    "not a per-model one. Enable it at "
    "https://openrouter.ai/settings/privacy, then re-run."
)


def _is_data_policy_error(text):
    text = str(text).lower()
    return "data policy" in text or "free model publication" in text


def call_llm(prompt, *, api_key, models, timeout, max_tokens=1800, log=print):
    if not api_key:
        raise RuntimeError("OPENROUTER_API_KEY is missing.")

    errors = []

    for model in models:
        headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "HTTP-Referer": "https://nventures.streamlit.app/",
            "X-Title": "nVentures Fund II Sourcing Assistant",
        }

        body = {
            "model": model,
            "messages": [{"role": "user", "content": prompt}],
            "temperature": 0.1,
            "max_tokens": max_tokens,
        }

        try:
            response = http_request_with_retry(
                "POST",
                OPENROUTER_URL,
                headers=headers,
                json_body=body,
                timeout=timeout,
                max_attempts=3,
                label=f"OpenRouter [{model}]",
                log=log,
            )

            data = response.json()

            if "error" in data:
                error = data["error"]
                if isinstance(error, dict):
                    code = error.get("code")
                    message = error.get("message")
                else:
                    code = None
                    message = str(error)

                if _is_data_policy_error(message):
                    log(f"      {model}: blocked by data policy - stopping.")
                    raise RuntimeError(DATA_POLICY_HELP)

                errors.append(f"{model}: {code} - {message}")
                continue

            choices = data.get("choices", [])
            if not choices:
                errors.append(f"{model}: no choices returned")
                continue

            content = choices[0].get("message", {}).get("content", "")
            if not content:
                errors.append(f"{model}: empty content")
                continue

            return content

        except RuntimeError as error:
            # A non-retryable HTTP error (e.g. 404) from http_request_with_retry
            # lands here as a RuntimeError whose text includes the response
            # body. Every ':free' model fails with the identical data-policy
            # message in that case, so stop immediately instead of grinding
            # through the rest of the fallback chain for no benefit.
            if _is_data_policy_error(error):
                log(f"      {model}: blocked by data policy - stopping.")
                raise RuntimeError(DATA_POLICY_HELP) from error

            errors.append(f"{model}: {error}")
            log(f"      Model failed: {model}")
            log(f"      {error}")
            continue

        except Exception as error:  # noqa: BLE001 - fall through to next model
            errors.append(f"{model}: {error}")
            log(f"      Model failed: {model}")
            log(f"      {error}")
            continue

    raise RuntimeError("All OpenRouter models failed.\n" + "\n".join(errors))


def safe_json_parse(text):
    if isinstance(text, dict):
        return text
    if not text:
        return {}

    text = str(text).strip()
    text = re.sub(r"^```(?:json)?\s*", "", text, flags=re.IGNORECASE)
    text = re.sub(r"\s*```$", "", text)

    try:
        result = json.loads(text)
        if isinstance(result, dict):
            return result
    except (ValueError, TypeError):
        pass

    start = text.find("{")
    end = text.rfind("}")

    if start >= 0 and end > start:
        try:
            result = json.loads(text[start:end + 1])
            if isinstance(result, dict):
                return result
        except (ValueError, TypeError):
            pass

    return {}


# ============================================================================
# TAVILY
# ============================================================================

def tavily_search(
    query,
    *,
    api_key,
    timeout,
    max_results,
    include_domains=None,
    log=print,
):
    if not api_key:
        raise RuntimeError("TAVILY_API_KEY is missing.")

    body = {
        "api_key": api_key,
        "query": query,
        "search_depth": "advanced",
        "max_results": max_results,
        "include_answer": False,
        "include_raw_content": False,
        "include_images": False,
    }

    if include_domains:
        body["include_domains"] = include_domains

    response = http_request_with_retry(
        "POST",
        TAVILY_URL,
        json_body=body,
        timeout=timeout,
        max_attempts=4,
        label="Tavily",
        log=log,
    )

    data = response.json()

    if "error" in data:
        raise RuntimeError(
            "Tavily error:\n" + json.dumps(data["error"], indent=2)
        )

    return data


def combined_raw_text(search_response, char_limit=None):
    if not search_response:
        return ""

    pieces = []

    answer = search_response.get("answer", "")
    if answer:
        pieces.append(str(answer))

    for result in search_response.get("results", []):
        title = normalize_text(result.get("title", ""))
        content = normalize_text(result.get("content", ""))
        url = normalize_text(result.get("url", ""))
        pieces.append(f"TITLE: {title}\nURL: {url}\nCONTENT: {content}")

    text = "\n\n".join(pieces)

    if char_limit:
        return text[:char_limit]

    return text


# ============================================================================
# AI RECORD CLEANING
# ============================================================================

FIELD_ALIASES = {
    "Founded Year": ["Founded Year", "Founded year", "Year Founded", "Founded"],
    "Product Description": [
        "Product Description",
        "Product Descriptoon",
        "Product",
    ],
    "Unique Value Propositions": [
        "Unique Value Propositions",
        "Unique Value Propsotions",
        "Value Proposition",
        "UVP",
    ],
    "Founder Name": ["Founder Name", "Founder", "Founders"],
    "Founder LinkedIn": [
        "Founder LinkedIn",
        "Founder Linkedin",
        "Founder LinkedIn URL",
    ],
    "Company LinkedIn": [
        "Company LinkedIn",
        "Company Linkedin",
        "LinkedIn",
        "LinkedIn URL",
    ],
    "Website": ["Website", "Company Website", "Website URL"],
    "Last Round": [
        "Last Round",
        "Latest Funding",
        "Latest Funding Round",
        "Last Funding Round",
    ],
    "Notable Investors": ["Notable Investors", "Investors", "Key Investors"],
    "Number of Customers": ["Number of Customers", "Customer Count"],
    "Notable Customers": ["Notable Customers", "Key Customers"],
    "Team Size": [
        "Team Size",
        "Employees",
        "Employee Count",
        "Number of Employees",
    ],
}


def clean_ai_record(record):
    if not isinstance(record, dict):
        return {}

    cleaned = {}

    for key, value in record.items():
        key = normalize_text(key)

        if value is None:
            value = ""
        elif isinstance(value, list):
            value = ", ".join(
                normalize_text(item) for item in value if normalize_text(item)
            )
        elif isinstance(value, dict):
            value = json.dumps(value, ensure_ascii=False)
        else:
            value = normalize_text(value)

        markdown = re.match(r"^\[.*?\]\((https?://[^)]+)\)$", value)
        if markdown:
            value = markdown.group(1)

        cleaned[key] = value

    for canonical, possible in FIELD_ALIASES.items():
        if canonical in cleaned:
            continue
        for candidate in possible:
            if candidate in cleaned:
                cleaned[canonical] = cleaned[candidate]
                break

    return cleaned


# ============================================================================
# DETERMINISTIC FUNDING / MATURITY FILTER
# ============================================================================

MONEY_PATTERNS = [
    (r"\$\s*([\d,.]+)\s*(billion|bn)\b", 1_000_000_000),
    (r"\$\s*([\d,.]+)\s*(million|mn|m)\b", 1_000_000),
    (r"\$\s*([\d,.]+)\s*(thousand|k)\b", 1_000),
    (r"usd\s*([\d,.]+)\s*(billion|bn)\b", 1_000_000_000),
    (r"usd\s*([\d,.]+)\s*(million|mn|m)\b", 1_000_000),
    (r"usd\s*([\d,.]+)\s*(thousand|k)\b", 1_000),
]

MATURE_TERMS = [
    "series b",
    "series c",
    "series d",
    "series e",
    "series f",
    "series g",
    "series h",
    "ipo",
    "publicly listed",
    "public company",
    "late stage",
    "growth stage",
    "pre-ipo",
]

# Phrases within a short window before a match that flip its meaning, e.g.
# "no plans for an IPO" or "not raising a Series A". Checked as a plain
# substring of the preceding text, so keep entries lowercase and simple.
NEGATION_CUES = [
    "no ",
    "not ",
    "never ",
    "without ",
    "unlikely ",
    "no plans for",
    "no plans to",
    "not planning",
    "not currently",
    "does not",
    "doesn't",
    "did not",
    "didn't",
    "has not",
    "hasn't",
    "have not",
    "haven't",
    "isn't",
    "is not",
    "rules out",
    "ruled out",
    "denies",
    "denied",
    "no current plans",
    "unlike",
]

NEGATION_WINDOW_CHARS = 60
SNIPPET_CONTEXT_CHARS = 60


def _is_negated(combined_lower, match_start):
    """True if a negation cue appears shortly before the match.

    A plain 'term in text' substring check has no concept of context, so it
    treats "no plans for an IPO" the same as an actual IPO. This looks at
    the text immediately preceding the match for a negating phrase.
    """
    context_start = max(0, match_start - NEGATION_WINDOW_CHARS)
    return any(
        cue in combined_lower[context_start:match_start] for cue in NEGATION_CUES
    )


def _find_term_match(combined, combined_lower, term):
    """Find an un-negated, word-bounded occurrence of term.

    Returns a short snippet of surrounding text for logging, or None.
    Word boundaries matter: a plain substring check on 'ipo' matches inside
    ordinary words like 'Chipotle' or 'shipowner', producing false
    maturity rejections that have nothing to do with the candidate company.
    """
    pattern = re.compile(r"\b" + re.escape(term) + r"\b")

    for match in pattern.finditer(combined_lower):
        if _is_negated(combined_lower, match.start()):
            continue

        start = max(0, match.start() - SNIPPET_CONTEXT_CHARS)
        end = min(len(combined), match.end() + SNIPPET_CONTEXT_CHARS)
        return normalize_text(combined[start:end])

    return None


def parse_money_values(text):
    text = normalize_text(text).lower()
    if not text:
        return []

    values = []

    for pattern, multiplier in MONEY_PATTERNS:
        for match in re.finditer(pattern, text):
            try:
                number = float(match.group(1).replace(",", ""))
                values.append(number * multiplier)
            except (TypeError, ValueError):
                pass

    return values


def funding_check(record, research_text, max_total_funding):
    combined = " ".join(
        [
            normalize_text(record.get("Last Round", "")),
            normalize_text(record.get("Stage", "")),
            normalize_text(research_text),
        ]
    )

    combined_lower = combined.lower()

    # Reject clearly mature funding stages.
    # Series A is intentionally NOT included here because a company can
    # mention a planned/future Series A while still being seed-stage.
    for term in MATURE_TERMS:
        snippet = _find_term_match(combined, combined_lower, term)
        if snippet:
            return False, (
                f"Maturity/funding evidence contains '{term}': "
                f"\"...{snippet}...\""
            )

    # Only reject Series A when the evidence indicates that the company
    # has actually completed/raised/closed a Series A.
    series_a_patterns = [
        r"\braised\s+(?:a\s+)?series\s+a\b",
        r"\bclosed\s+(?:a\s+)?series\s+a\b",
        r"\bcompleted\s+(?:a\s+)?series\s+a\b",
        r"\bannounced\s+(?:a\s+)?series\s+a\b",
        r"\bseries\s+a\s+round\b",
        r"\bseries\s+a\s+funding\b",
        r"\bseries\s+a\s+financing\b",
        r"\bseries\s+a\s+of\s+\$",
    ]

    for pattern in series_a_patterns:
        match = re.search(pattern, combined_lower)

        if match and not _is_negated(combined_lower, match.start()):
            start = max(0, match.start() - SNIPPET_CONTEXT_CHARS)
            end = min(len(combined), match.end() + SNIPPET_CONTEXT_CHARS)
            snippet = normalize_text(combined[start:end])

            return False, (
                f"Completed Series A evidence: \"...{snippet}...\""
            )

    # Funding amount check.
    money_values = parse_money_values(combined)

    if money_values:
        maximum = max(money_values)

        if maximum > max_total_funding:
            return False, (
                f"Funding evidence around ${maximum:,.0f} exceeds the "
                f"${max_total_funding:,.0f} limit."
            )

    return True, ""


def maturity_flag(record, research_text, max_total_funding):
    combined = " ".join(
        [
            normalize_text(record.get("Stage", "")),
            normalize_text(record.get("Last Round", "")),
            normalize_text(research_text),
        ]
    )

    combined_lower = combined.lower()

    warnings = []

    if _find_term_match(combined, combined_lower, "series a"):
        warnings.append("mentions 'Series A'")
    if _find_term_match(combined, combined_lower, "series b"):
        warnings.append("mentions 'Series B'")

    values = parse_money_values(combined)
    if values:
        maximum = max(values)
        if maximum > max_total_funding:
            warnings.append(f"funding around ${maximum:,.0f}")

    if warnings:
        return "Maturity check flagged: " + "; ".join(warnings) + "."

    return ""


# ============================================================================
# PARTNER CONTROL
# ============================================================================

def get_control_state(worksheet):
    values = worksheet.get_all_values()
    state = {}

    if len(values) <= 1:
        return state

    for row in values[1:]:
        if not row:
            continue

        name = normalize_text(row[0] if len(row) > 0 else "")
        if not name:
            continue

        last_sourced = normalize_text(row[1] if len(row) > 1 else "")

        try:
            run_count = int(row[2] if len(row) > 2 else 0)
        except (TypeError, ValueError):
            run_count = 0

        try:
            last_added = int(row[3] if len(row) > 3 else 0)
        except (TypeError, ValueError):
            last_added = 0

        state[name] = {
            "last_sourced": last_sourced,
            "run_count": run_count,
            "last_added": last_added,
        }

    return state


def choose_partners(partners, control_state, maximum, respect_relevance_flag=True):
    """Least recently sourced partners first, ties broken randomly."""
    eligible = []

    for partner in partners:
        name = normalize_text(partner.get("Company Name", ""))
        if not name:
            continue

        if respect_relevance_flag:
            relevance = normalize_text(
                partner.get("nVentures Relevance", "")
            ).lower()
            if relevance == "no":
                continue

        state = control_state.get(name, {})
        last_sourced = normalize_text(state.get("last_sourced", ""))

        if not last_sourced:
            timestamp = 0
        else:
            try:
                parsed = datetime.fromisoformat(
                    last_sourced.replace("Z", "+00:00")
                )
                timestamp = parsed.timestamp()
            except ValueError:
                timestamp = 9999999999

        eligible.append((timestamp, random.random(), partner))

    eligible.sort(key=lambda x: (x[0], x[1]))

    return [item[2] for item in eligible[:maximum]]


def update_control_state(worksheet, partner_name, added_count):
    now = datetime.now(timezone.utc).isoformat()
    values = worksheet.get_all_values()

    existing_row = None
    previous_runs = 0

    for i, row in enumerate(values[1:], start=2):
        if row and normalize_text(row[0]).lower() == partner_name.lower():
            existing_row = i
            try:
                previous_runs = int(row[2] if len(row) > 2 else 0)
            except (TypeError, ValueError):
                previous_runs = 0
            break

    new_row = [partner_name, now, previous_runs + 1, added_count]

    if existing_row:
        worksheet.update(
            f"A{existing_row}:D{existing_row}",
            [new_row],
            value_input_option="USER_ENTERED",
        )
    else:
        worksheet.append_row(new_row, value_input_option="USER_ENTERED")


# ============================================================================
# BUILD GOOGLE SHEETS ROW
# ============================================================================

# Left: canonical field name from the AI record.
# Right: the header exactly as it appears in the Active Sourcing sheet,
# including its existing typos and trailing spaces.
FIELD_MAPPING = {
    "Company Name": "Company Name",
    "Country": "Country",
    "Sector": "Sector",
    "Business Model": "Business Model",
    "Stage": "Stage",
    "Founded Year": "Founded year",
    "Headquarters": "Headquarters",
    "Website": "Website",
    "Company LinkedIn": "Company LinkedIn",
    "Company Description": "Company Description",
    "Product Description": "Product Descriptoon",
    "Target Market": "Target Market",
    "Team Size": "Team Size",
    "Number of Customers": "Number of Customers",
    "Notable Customers": "Notable Customers",
    "Revenue Generated": "Revenue Generated",
    "Unique Value Propositions": "Unique Value Propsotions",
    "Competitors": "Competitors",
    "Vision": "Vision",
    "Founder Name": "Founder Name ",
    "Founder LinkedIn": "Founder LinkedIn",
    "Last Round": "Last Round",
    "Notable Investors": "Notable Investors",
}


def build_sheet_row(headers, record, partner_name, verification, maturity_note):
    row = [""] * len(headers)
    first_idx, all_idx = build_header_index(headers)

    populated = []

    for field, header in FIELD_MAPPING.items():
        value = normalize_text(record.get(field, ""))
        if not value:
            continue

        col = get_col(header, first_idx)
        if not col:
            continue

        row[col - 1] = value
        populated.append(field)

    for header, value in (
        ("Outreach status", "Not Reached Out"),
        ("Status", "Sourced"),
        ("IC Recommendation", "Potential Fit"),
    ):
        col = get_col(header, first_idx)
        if col:
            row[col - 1] = value

    # Reason
    reason_parts = []

    if verification.get("b2b", False):
        reason_parts.append("B2B")
    if verification.get("early_stage", False):
        reason_parts.append("early-stage")
    if verification.get("funding_within_limit", False):
        reason_parts.append("funding within threshold")

    verification_reason = normalize_text(verification.get("reason", ""))
    if verification_reason:
        reason_parts.append(verification_reason)

    col = get_col("Reason", first_idx)
    if col:
        row[col - 1] = "; ".join(reason_parts)

    # Extra Notes
    note = (
        f"Sourced via {partner_name}. "
        f"AI-assisted research; verify before IC/outreach."
    )

    if maturity_note:
        note += " " + maturity_note

    notes_cols = all_idx.get(normalize_header("Extra Notes"), [])

    if notes_cols:
        row[notes_cols[0] - 1] = note

    if len(notes_cols) >= 2:
        row[notes_cols[1] - 1] = f"Verification: {verification_reason}"

    # Updates
    col = get_col("Updates", first_idx)
    if col:
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
        row[col - 1] = f"Sourced {timestamp} via {partner_name}."

    return row, populated


# ============================================================================
# WRITE + READ-BACK VERIFICATION
# ============================================================================

def append_and_verify_row(worksheet, row, headers, company_name, log=print):
    if len(row) != len(headers):
        raise RuntimeError(
            f"Row width mismatch: {len(row)} cells vs "
            f"{len(headers)} sheet columns."
        )

    company_col = None

    for i, header in enumerate(headers):
        if normalize_header(header) == "company name":
            company_col = i
            break

    if company_col is None:
        raise RuntimeError(
            "Could not find Company Name column during write."
        )

    target_name = normalize_company_name(company_name)

    log("      Preparing SAFE INSERT...")

    # ---------------------------------------------------------
    # 1. READ THE COMPLETE SHEET BEFORE WRITING
    # ---------------------------------------------------------

    before_values = worksheet.get_all_values()

    if not before_values:
        raise RuntimeError(
            "Cannot safely write: worksheet returned no rows."
        )

    # Find the LAST ACTUALLY POPULATED ROW.
    #
    # We inspect the entire row rather than relying on
    # worksheet.row_count or append_row().
    last_data_row = 1

    for row_number, existing_row in enumerate(
        before_values,
        start=1,
    ):
        if any(normalize_text(value) for value in existing_row):
            last_data_row = row_number

    insert_at = last_data_row + 1

    log(
        f"      Last populated row: {last_data_row}"
    )

    log(
        f"      Will INSERT new row at: {insert_at}"
    )

    # ---------------------------------------------------------
    # 2. CHECK THAT COMPANY DOES NOT ALREADY EXIST
    # ---------------------------------------------------------

    existing_matches = []

    for row_number, existing_row in enumerate(
        before_values[1:],
        start=2,
    ):
        if len(existing_row) <= company_col:
            continue

        existing_name = normalize_company_name(
            existing_row[company_col]
        )

        if existing_name == target_name:
            existing_matches.append(row_number)

    if existing_matches:
        raise RuntimeError(
            "SAFETY STOP: company already exists in sheet.\n"
            f"Company: {company_name}\n"
            f"Existing row(s): {existing_matches}"
        )

    # ---------------------------------------------------------
    # 3. INSERT A PHYSICAL NEW ROW
    # ---------------------------------------------------------

    log(
        f"      INSERTING physical sheet row {insert_at}..."
    )

    worksheet.insert_row(
        row,
        index=insert_at,
        value_input_option="USER_ENTERED",
    )

    log("      INSERT: successful")

    time.sleep(1)

    # ---------------------------------------------------------
    # 4. READ SHEET AGAIN
    # ---------------------------------------------------------

    after_values = worksheet.get_all_values()

    # ---------------------------------------------------------
    # 5. VERIFY ROW COUNT INCREASED
    # ---------------------------------------------------------

    if len(after_values) != len(before_values) + 1:
        raise RuntimeError(
            "SAFETY CHECK FAILED: unexpected row count after insert.\n"
            f"Before: {len(before_values)}\n"
            f"After: {len(after_values)}\n"
            f"Expected: {len(before_values) + 1}"
        )

    # ---------------------------------------------------------
    # 6. VERIFY EVERY PREVIOUS ROW IS STILL PRESENT
    #
    # Rows at/after insert_at should have shifted down by one.
    # Rows before insert_at should be identical.
    # ---------------------------------------------------------

    for old_row_number in range(1, len(before_values) + 1):

        old_row = before_values[old_row_number - 1]

        if old_row_number < insert_at:
            new_row_number = old_row_number
        else:
            new_row_number = old_row_number + 1

        new_row = after_values[new_row_number - 1]

        if old_row != new_row:
            raise RuntimeError(
                "CRITICAL SAFETY FAILURE: existing row changed "
                "during INSERT.\n"
                f"Original row: {old_row_number}\n"
                f"Expected new row: {new_row_number}\n"
                f"BEFORE: {old_row}\n"
                f"AFTER:  {new_row}"
            )

    log(
        "      SAFETY: all existing rows preserved."
    )

    # ---------------------------------------------------------
    # 7. VERIFY THE NEW COMPANY IS AT THE INSERTED ROW
    # ---------------------------------------------------------

    if len(after_values[insert_at - 1]) <= company_col:
        raise RuntimeError(
            "READ-BACK VERIFICATION FAILED: inserted row does "
            "not contain Company Name."
        )

    actual_company = normalize_text(
        after_values[insert_at - 1][company_col]
    )

    if normalize_company_name(actual_company) != target_name:
        raise RuntimeError(
            "READ-BACK VERIFICATION FAILED.\n"
            f"Expected: {company_name}\n"
            f"Found: {actual_company}\n"
            f"Expected row: {insert_at}"
        )

    # ---------------------------------------------------------
    # 8. VERIFY COMPANY EXISTS EXACTLY ONCE
    # ---------------------------------------------------------

    matching_rows = []

    for row_number, existing_row in enumerate(
        after_values[1:],
        start=2,
    ):
        if len(existing_row) <= company_col:
            continue

        existing_name = normalize_company_name(
            existing_row[company_col]
        )

        if existing_name == target_name:
            matching_rows.append(row_number)

    if matching_rows != [insert_at]:
        raise RuntimeError(
            "READ-BACK VERIFICATION FAILED: unexpected company "
            "location.\n"
            f"Company: {company_name}\n"
            f"Expected row: {insert_at}\n"
            f"Found rows: {matching_rows}"
        )

    log("      READ-BACK: successful")
    log(f"      Row verified: {insert_at}")
    log(f"      Company verified: {actual_company}")

    return insert_at
# ============================================================================
# PROMPTS
# ============================================================================

def candidate_prompt(partner_name, portfolio_text):
    return (
        "You are an investment sourcing analyst helping nVentures identify\n"
        "early-stage B2B startups for Fund II.\n\n"
        "PARTNER:\n"
        f"{partner_name}\n\n"
        "REQUIREMENTS:\n\n"
        "1. B2B:\n"
        "   Primarily sells to businesses, institutions or organizations.\n\n"
        "2. EARLY STAGE:\n"
        "   Prefer pre-seed or seed.\n\n"
        "   A company that is still pre-seed or seed should remain eligible "
        "even if the research mentions plans, discussions, or expectations "
        "for a future Series A.\n\n"
        "   Prefer companies whose latest completed funding round is "
        "pre-seed or seed.\n"
        "   Exclude clearly mature/growth companies.\n\n"
        "3. FUNDING:\n"
        "   Prefer companies with approximately $3M or less total funding.\n\n"
        "4. REAL COMPANY:\n"
        "   Must have credible evidence of an actual operating company/product.\n\n"
        "There is no geographic restriction. Companies headquartered anywhere "
        "in the world are eligible.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{{\n"
        '  "candidates": [\n'
        "    {{\n"
        '      "company_name": "Company Name",\n'
        '      "sector_guess": "Sector",\n'
        '      "why_it_fits": "Short factual explanation."\n'
        "    }}\n"
        "  ]\n"
        "}}\n\n"
        "If no suitable candidates exist:\n\n"
        "{{\n"
        '  "candidates": []\n'
        "}}\n\n"
        "WEB CONTENT:\n"
        f"{portfolio_text}\n"
    )


def research_prompt(candidate_name, initial_context, deep_text):
    return (
        "You are conducting structured research on a startup for an investment\n"
        "pipeline.\n\n"
        "Company:\n"
        f"{candidate_name}\n\n"
        "Initial reason:\n"
        f"{initial_context}\n\n"
        "Use ONLY facts supported by the supplied research.\n\n"
        "IMPORTANT:\n"
        "- Never invent facts.\n"
        "- Never guess.\n"
        '- Missing information must be "".\n'
        "- Headquarters must be the company's actual headquarters.\n"
        "- Country must be the country of the company's headquarters.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{{\n"
        '  "Company Name": "",\n'
        '  "Country": "",\n'
        '  "Sector": "",\n'
        '  "Business Model": "",\n'
        '  "Stage": "",\n'
        '  "Founded Year": "",\n'
        '  "Headquarters": "",\n'
        '  "Website": "",\n'
        '  "Company LinkedIn": "",\n'
        '  "Company Description": "",\n'
        '  "Product Description": "",\n'
        '  "Target Market": "",\n'
        '  "Team Size": "",\n'
        '  "Number of Customers": "",\n'
        '  "Notable Customers": "",\n'
        '  "Revenue Generated": "",\n'
        '  "Unique Value Propositions": "",\n'
        '  "Competitors": "",\n'
        '  "Vision": "",\n'
        '  "Founder Name": "",\n'
        '  "Founder LinkedIn": "",\n'
        '  "Last Round": "",\n'
        '  "Notable Investors": ""\n'
        "}}\n\n"
        "RESEARCH:\n"
        f"{deep_text}\n"
    )

def verification_prompt(researched_name, deep_text):
    return (
        "You are the final screening analyst for an early-stage VC sourcing\n"
        "pipeline.\n\n"
        "Company:\n"
        f"{researched_name}\n\n"
        "Research:\n"
        f"{deep_text}\n\n"
        "Evaluate ONLY using the supplied research.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{{\n"
        '  "b2b": true,\n'
        '  "early_stage": true,\n'
        '  "funding_within_limit": true,\n'
        '  "active_company": true,\n'
        '  "too_mature": false,\n'
        '  "confidence": "high",\n'
        '  "reason": "Short factual explanation"\n'
        "}}\n\n"
        "Rules:\n\n"
        "B2B:\n"
        "The company must primarily sell to businesses/institutions.\n\n"
        "CURRENT FUNDING STAGE:\n"
        "Evaluate the company's actual current/latest completed funding stage.\n\n"
        "Pre-seed = true.\n"
        "Seed = true.\n\n"
        "A company should NOT be rejected merely because the research mentions:\n"
        "- a future Series A\n"
        "- plans to raise a Series A\n"
        "- preparing for a Series A\n"
        "- expectations of a Series A\n"
        "- investors discussing a possible Series A\n"
        "- speculation about a future Series A\n\n"
        "A completed, announced, or closed Series A = false.\n\n"
        "Series B or later = false.\n\n"
        "Clearly mature/growth-stage company = false.\n\n"
        "FUNDING:\n"
        "Approximately $3M total funding or less = true.\n"
        "Clearly above $3M total funding = false.\n\n"
        "ACTIVE:\n"
        "There should be evidence of an actual operating company.\n\n"
        "Do NOT treat missing evidence as positive evidence.\n\n"
        "When determining the current stage, prioritize:\n"
        "1. The latest completed funding round.\n"
        "2. Explicit statements about the company's current stage.\n"
        "3. Funding dates and amounts.\n"
        "4. Distinguish completed funding from planned or future fundraising.\n\n"
        'Confidence: "high", "medium", or "low".\n'
    )

# ============================================================================
# MAIN ENTRY POINT
# ============================================================================

def run_sourcing(
    *,
    sourcing_ws,
    partner_ws,
    control_ws,
    openrouter_api_key,
    tavily_api_key,
    openrouter_model="minimax/minimax-m3",
    target_companies=25,
    max_partners=12,
    max_candidates_per_partner=10,
    max_deep_research=45,
    max_total_funding=3_000_000,
    max_team_size_warning=30,
    tavily_timeout=60,
    openrouter_timeout=90,
    max_tavily_results=5,
    max_research_chars=14000,
    request_delay=1.0,
    require_b2b=True,
    require_early_stage=True,
    respect_relevance_flag=True,
    progress_callback=None,
    log_callback=None,
):
    """Run the sourcing engine and return a structured, JSON-safe report."""

    if not openrouter_api_key:
        raise RuntimeError("OPENROUTER_API_KEY is missing.")
    if not tavily_api_key:
        raise RuntimeError("TAVILY_API_KEY is missing.")

    target_companies = int(target_companies)
    max_partners = int(max_partners)
    max_candidates_per_partner = int(max_candidates_per_partner)
    max_deep_research = int(max_deep_research)
    max_total_funding = float(max_total_funding)
    request_delay = float(request_delay)

    log_lines = []

    def log(message=""):
        stamp = datetime.now(timezone.utc).strftime("%H:%M:%S")
        text = f"[{stamp}] {message}"
        log_lines.append(text)
        print(text, flush=True)

        if log_callback:
            try:
                log_callback(text)
            except Exception:  # noqa: BLE001 - UI must never kill the run
                pass

    def report_progress(percent, message=""):
        if message:
            log(message)
        if progress_callback:
            try:
                progress_callback(max(0, min(100, int(percent))), message)
            except Exception:  # noqa: BLE001 - UI must never kill the run
                pass

    models = build_model_order(openrouter_model)
    log("OpenRouter model order: " + ", ".join(models))

    def llm(prompt, max_tokens=1800):
        return call_llm(
            prompt,
            api_key=openrouter_api_key,
            models=models,
            timeout=openrouter_timeout,
            max_tokens=max_tokens,
            log=log,
        )

    def search(query, include_domains=None, max_results=None):
        return tavily_search(
            query,
            api_key=tavily_api_key,
            timeout=tavily_timeout,
            max_results=max_results or max_tavily_results,
            include_domains=include_domains,
            log=log,
        )

    # ------------------------------------------------------------------
    # Sheets setup
    # ------------------------------------------------------------------

    report_progress(2, "Reading Active Sourcing headers...")

    sourcing_headers = sourcing_ws.row_values(1)
    if not sourcing_headers:
        raise RuntimeError("Active Sourcing has no header row.")

    log(f"Active Sourcing columns: {len(sourcing_headers)}")

    report_progress(5, "Building existing company index...")

    existing_indexes = build_existing_indexes(sourcing_ws)

    log(f"Existing company names: {len(existing_indexes['names'])}")
    log(f"Existing domains: {len(existing_indexes['domains'])}")
    log(f"Existing LinkedIn URLs: {len(existing_indexes['linkedin'])}")

    partners = worksheet_rows_as_dicts(partner_ws)
    if not partners:
        raise RuntimeError("No partners were found.")

    control_state = get_control_state(control_ws)
    partners_to_process = choose_partners(
        partners,
        control_state,
        max_partners,
        respect_relevance_flag=respect_relevance_flag,
    )

    log(
        "Partners selected: "
        + ", ".join(
            normalize_text(p.get("Company Name", ""))
            for p in partners_to_process
        )
    )

    # ------------------------------------------------------------------
    # Run state
    # ------------------------------------------------------------------

    run_started = datetime.now(timezone.utc)

    accepted_companies = []
    accepted_details = []
    rejected_candidates = []
    skipped_duplicates = []
    failed_partners = []
    partner_stats = {}

    deep_research_count = 0
    candidate_count = 0

    total_partners = max(1, len(partners_to_process))

    # ------------------------------------------------------------------
    # Main partner loop
    # ------------------------------------------------------------------

    for partner_index, partner in enumerate(partners_to_process):
        if len(accepted_companies) >= target_companies:
            break

        partner_name = normalize_text(partner.get("Company Name", ""))
        if not partner_name:
            continue

        report_progress(
            5 + int(90 * partner_index / total_partners),
            f"Partner {partner_index + 1}/{total_partners}: {partner_name}",
        )

        partner_added = 0

        try:
            # --- portfolio URL ---
            portfolio_url = ""
            for field in (
                "Company Portfolio",
                "Portfolio",
                "Portfolio URL",
                "Website",
                "Company Website",
            ):
                value = normalize_text(partner.get(field, ""))
                if value:
                    portfolio_url = value
                    break

            portfolio_domain = normalize_domain(portfolio_url)

            # --- portfolio search ---
            if portfolio_domain:
                log(f"Portfolio domain: {portfolio_domain}")
                portfolio_result = search(
                    "portfolio companies startups",
                    include_domains=[portfolio_domain],
                )
            else:
                log("No portfolio URL found. Using general partner search.")
                portfolio_result = search(
                    f'"{partner_name}" portfolio companies startups B2B seed'
                )

            portfolio_text = combined_raw_text(
                portfolio_result, char_limit=max_research_chars
            )

            if not portfolio_text:
                log("No usable portfolio research.")
                partner_stats[partner_name] = 0
                update_control_state(control_ws, partner_name, 0)
                continue

            # --- candidate extraction ---
            log("Running AI candidate extraction...")

            extraction = safe_json_parse(
                llm(
                    candidate_prompt(partner_name, portfolio_text),
                    max_tokens=4000,
                )
            )

            candidates = extraction.get("candidates", [])
            if not isinstance(candidates, list):
                candidates = []

            candidates = candidates[:max_candidates_per_partner]

            log(f"AI returned {len(candidates)} candidates.")

            # --- candidate loop ---
            for candidate in candidates:
                if len(accepted_companies) >= target_companies:
                    break
                if deep_research_count >= max_deep_research:
                    break
                if not isinstance(candidate, dict):
                    continue

                candidate_count += 1

                candidate_name = normalize_text(
                    candidate.get("company_name", "")
                )
                if not candidate_name:
                    continue

                log(f"Candidate #{candidate_count}: {candidate_name}")

                if company_is_existing(
                    candidate_name, indexes=existing_indexes
                ):
                    log("  SKIP - duplicate already in Google Sheets.")
                    skipped_duplicates.append(candidate_name)
                    continue

                initial_context = normalize_text(
                    candidate.get("why_it_fits", "")
                )
                sector_guess = normalize_text(candidate.get("sector_guess", ""))

                log(f"  Sector guess: {sector_guess or 'unknown'}")

                # --- deep research ---
                deep_research_count += 1
                time.sleep(request_delay)

                log("  Running deep company research...")

                deep_result = search(
                    f'"{candidate_name}" company headquarters founders '
                    f"funding seed B2B customers investors"
                )

                deep_text = combined_raw_text(
                    deep_result, char_limit=max_research_chars
                )

                if not deep_text:
                    log("  REJECT - no usable research.")
                    rejected_candidates.append(
                        (candidate_name, "No usable research")
                    )
                    continue

                # --- structured research ---
                record = clean_ai_record(
                    safe_json_parse(
                        llm(
                            research_prompt(
                                candidate_name, initial_context, deep_text
                            ),
                            max_tokens=5000,
                        )
                    )
                )

                if not normalize_text(record.get("Company Name", "")):
                    record["Company Name"] = candidate_name

                if not normalize_text(record.get("Sector", "")):
                    record["Sector"] = sector_guess

                website = normalize_text(record.get("Website", ""))
                company_linkedin = normalize_text(
                    record.get("Company LinkedIn", "")
                )

                researched_name = (
                    normalize_text(record.get("Company Name", ""))
                    or candidate_name
                )

                # --- second duplicate check ---
                if company_is_existing(
                    researched_name,
                    website=website,
                    linkedin=company_linkedin,
                    indexes=existing_indexes,
                ):
                    log("  SKIP - duplicate after research.")
                    skipped_duplicates.append(researched_name)
                    continue

                # --- deterministic funding / maturity filter ---
                funding_ok, funding_reason = funding_check(
                    record, deep_text, max_total_funding
                )

                if not funding_ok:
                    log("  REJECTED - funding/maturity.")
                    log(f"  {funding_reason}")
                    rejected_candidates.append(
                        (researched_name, funding_reason)
                    )
                    continue

                # --- AI verification ---
                log("  Running final qualification...")

                verification = safe_json_parse(
                    llm(
                        verification_prompt(researched_name, deep_text),
                        max_tokens=3000,
                    )
                )

                b2b = verification.get("b2b", False) is True
                early_stage = verification.get("early_stage", False) is True
                funding_within_limit = (
                    verification.get("funding_within_limit", False) is True
                )
                active_company = (
                    verification.get("active_company", False) is True
                )
                too_mature = verification.get("too_mature", True) is True

                confidence = normalize_text(
                    verification.get("confidence", "")
                ).lower()

                failures = []

                if require_b2b and not b2b:
                    failures.append("B2B requirement not verified")
                if require_early_stage and not early_stage:
                    failures.append("Early-stage requirement not verified")
                if not funding_within_limit:
                    failures.append("Funding requirement not satisfied")
                if not active_company:
                    failures.append("Active-company status not verified")
                if too_mature:
                    failures.append("Company appears too mature")
                if confidence == "low":
                    failures.append("Verification confidence is low")

                if failures:
                    log("  REJECTED")
                    for failure in failures:
                        log(f"    - {failure}")
                    rejected_candidates.append(
                        (researched_name, "; ".join(failures))
                    )
                    continue

                maturity_note = maturity_flag(
                    record, deep_text, max_total_funding
                )

                # --- accept ---
                log(f"  ACCEPTED - {researched_name}")

                team_size_digits = re.findall(
                    r"\d+", normalize_text(record.get("Team Size", ""))
                )
                if team_size_digits:
                    try:
                        if int(team_size_digits[0]) > int(max_team_size_warning):
                            log(
                                f"  WARNING: team size {team_size_digits[0]} "
                                f"exceeds {max_team_size_warning}"
                            )
                    except (TypeError, ValueError):
                        pass

                if maturity_note:
                    log(f"  WARNING: {maturity_note}")

                row, populated_fields = build_sheet_row(
                    sourcing_headers,
                    record,
                    partner_name,
                    verification,
                    maturity_note,
                )

                if len(row) != len(sourcing_headers):
                    raise RuntimeError(
                        f"Row width mismatch for {researched_name}: "
                        f"{len(row)} vs {len(sourcing_headers)}"
                    )

                log("  Writing company to Google Sheets...")

                written_row_number = append_and_verify_row(
                    sourcing_ws,
                    row,
                    sourcing_headers,
                    researched_name,
                    log=log,
                )

                log(f"  CONFIRMED IN GOOGLE SHEETS - row {written_row_number}")

                add_company_to_indexes(
                    researched_name,
                    website=website,
                    linkedin=company_linkedin,
                    indexes=existing_indexes,
                )

                accepted_companies.append(researched_name)

                accepted_details.append(
                    {
                        "company": researched_name,
                        "country": record.get("Country", ""),
                        "headquarters": record.get("Headquarters", ""),
                        "sector": record.get("Sector", ""),
                        "stage": record.get("Stage", ""),
                        "partner": partner_name,
                        "row": written_row_number,
                        "fields": len(populated_fields),
                    }
                )

                partner_added += 1

                report_progress(
                    5 + int(90 * partner_index / total_partners),
                    f"Added {len(accepted_companies)}/{target_companies}: "
                    f"{researched_name}",
                )

                try:
                    update_control_state(
                        control_ws, partner_name, partner_added
                    )
                except Exception as control_error:  # noqa: BLE001
                    log(f"  Warning: control update failed: {control_error}")

                if len(accepted_companies) >= target_companies:
                    log("TARGET REACHED.")
                    break

                time.sleep(request_delay)

            partner_stats[partner_name] = partner_added

            log(f"Partner complete: {partner_name} - {partner_added} new")

            try:
                update_control_state(control_ws, partner_name, partner_added)
            except Exception as control_error:  # noqa: BLE001
                log(f"Warning: control update failed: {control_error}")

        except Exception as partner_error:  # noqa: BLE001 - never kill the run
            log(f"!! PARTNER ERROR - {partner_name}: {partner_error}")
            failed_partners.append((partner_name, str(partner_error)))
            continue

    # ------------------------------------------------------------------
    # Final report
    # ------------------------------------------------------------------

    run_finished = datetime.now(timezone.utc)

    report_progress(
        100,
        f"Run complete - {len(accepted_companies)}/{target_companies} added.",
    )

    return {
        "started_at": run_started.isoformat(),
        "finished_at": run_finished.isoformat(),
        "runtime_seconds": (run_finished - run_started).total_seconds(),
        "target": target_companies,
        "accepted": accepted_companies,
        "accepted_details": accepted_details,
        "rejected": rejected_candidates,
        "duplicates": skipped_duplicates,
        "partner_errors": failed_partners,
        "deep_research_calls": deep_research_count,
        "candidates_considered": candidate_count,
        "partners_processed": len(partner_stats),
        "partner_breakdown": partner_stats,
        "log": log_lines,
    }
