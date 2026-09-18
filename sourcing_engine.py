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
  - Extracts portfolio candidates via FreeSerp + OpenRouter
  - Researches each candidate
  - Enforces B2B / active-company / maturity rules
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
from urllib.parse import quote_plus

OPENROUTER_URL = "https://openrouter.ai/api/v1/chat/completions"
FREESEPR_URL = "https://freeserp.ai/api.php"
# FreeSerp is keyless; tavily_api_key remains only as a backwards-compatible parameter.

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


def safe_json_parse(text):
    """Safely parse JSON returned by the LLM."""
    if not text:
        raise ValueError("Empty LLM response.")

    cleaned = str(text).strip()
    cleaned = re.sub(r"^```(?:json)?\s*", "", cleaned, flags=re.IGNORECASE)
    cleaned = re.sub(r"\s*```$", "", cleaned).strip()

    try:
        return json.loads(cleaned)
    except json.JSONDecodeError:
        pass

    # First try decoding from the first JSON object/array. This handles models
    # that prepend a short explanation or markdown despite the JSON-only prompt.
    for opener, decoder in (("{", "object"), ("[", "array")):
        start = cleaned.find(opener)
        if start == -1:
            continue
        try:
            value, _ = json.JSONDecoder().raw_decode(cleaned[start:])
            if decoder == "object" and isinstance(value, dict):
                return value
            if decoder == "array" and isinstance(value, list):
                return value
        except json.JSONDecodeError:
            pass

    # Some providers occasionally leave a trailing comma before a closing
    # brace/array. Fix only that narrow JSON formatting error; never attempt
    # to invent missing fields or complete truncated content.
    repaired = re.sub(r",(\s*[}\]])", r"\1", cleaned)
    if repaired != cleaned:
        try:
            return json.loads(repaired)
        except json.JSONDecodeError:
            pass

    raise ValueError(
        "Could not parse valid JSON from LLM response. "
        f"Response preview: {cleaned[:500]}"
    )


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
    timeout=120,
    max_attempts=3,
    label="HTTP request",
    log=print,
):
    last_error = None

    for attempt in range(1, max_attempts + 1):
        try:
            log(
                f"      {label}: attempt "
                f"{attempt}/{max_attempts}"
            )

            response = requests.request(
                method,
                url,
                headers=headers,
                json=json_body,
                timeout=timeout,
            )

            # ---------------------------------------------------------
            # SUCCESS
            # ---------------------------------------------------------

            if response.status_code < 400:
                return response

            # ---------------------------------------------------------
            # RETRYABLE HTTP ERRORS
            # ---------------------------------------------------------

            # OpenRouter can return HTTP 402 when the account's temporary
            # in-flight budget is exhausted. This is NOT a permanent
            # insufficient-credit error: the response tells us how long to
            # wait via Retry-After. Treat only this specific 402 condition as
            # retryable; ordinary 402 credit errors should fail immediately.
            is_openrouter_inflight_402 = (
                response.status_code == 402
                and "in_flight_budget_exhausted" in response.text
            )

            if response.status_code in {
                429,
                500,
                502,
                503,
                504,
            } or is_openrouter_inflight_402:
                retry_after = response.headers.get("Retry-After")

                if retry_after:
                    try:
                        wait_seconds = float(retry_after)
                    except (TypeError, ValueError):
                        wait_seconds = min(30, 2 ** attempt)
                else:
                    wait_seconds = min(30, 2 ** attempt)

                # OpenRouter may legitimately ask us to wait 120+ seconds
                # while earlier requests settle. Honor that server value
                # instead of immediately trying another model.
                if is_openrouter_inflight_402 and retry_after:
                    try:
                        wait_seconds = max(wait_seconds, float(retry_after))
                    except (TypeError, ValueError):
                        pass

                wait_seconds += random.uniform(0.25, 1.0)

                last_error = RuntimeError(
                    f"{label} returned HTTP "
                    f"{response.status_code}"
                )

                if attempt < max_attempts:
                    log(
                        f"      {label}: HTTP "
                        f"{response.status_code}. "
                        f"Retrying in "
                        f"{wait_seconds:.1f}s..."
                    )

                    time.sleep(wait_seconds)
                    continue

                log(
                    f"      {label}: HTTP "
                    f"{response.status_code} after "
                    f"{max_attempts} attempts."
                )

                break

            # ---------------------------------------------------------
            # NON-RETRYABLE HTTP ERROR
            # ---------------------------------------------------------

            raise RuntimeError(
                f"{label} failed with HTTP "
                f"{response.status_code}:\n"
                f"{response.text[:3000]}"
            )

        except requests.Timeout as error:
            last_error = error

            log(
                f"      {label}: TIMEOUT on attempt "
                f"{attempt}/{max_attempts}."
            )

            if attempt < max_attempts:
                wait_seconds = 2 + random.uniform(0.25, 1.0)

                log(
                    f"      Retrying in "
                    f"{wait_seconds:.1f}s..."
                )

                time.sleep(wait_seconds)

        except requests.ConnectionError as error:
            last_error = error

            log(
                f"      {label}: CONNECTION ERROR on "
                f"attempt {attempt}/{max_attempts}."
            )

            if attempt < max_attempts:
                wait_seconds = 2 + random.uniform(0.25, 1.0)

                log(
                    f"      Retrying in "
                    f"{wait_seconds:.1f}s..."
                )

                time.sleep(wait_seconds)

        except requests.RequestException as error:
            last_error = error

            log(
                f"      {label}: network error on "
                f"attempt {attempt}/{max_attempts}: "
                f"{error}"
            )

            if attempt < max_attempts:
                wait_seconds = 2 + random.uniform(0.25, 1.0)

                log(
                    f"      Retrying in "
                    f"{wait_seconds:.1f}s..."
                )

                time.sleep(wait_seconds)

    raise RuntimeError(
        f"{label} failed after "
        f"{max_attempts} attempts: "
        f"{last_error}"
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


def call_llm(
    prompt,
    *,
    api_key,
    models,
    timeout,
    max_tokens=1800,
    log=print,
):
    if not api_key:
        raise RuntimeError("OPENROUTER_API_KEY is missing.")

    # Keep requests within a low/free OpenRouter credit balance.
    # OpenRouter rejects the entire request if max_tokens exceeds the
    # remaining affordable completion budget.
    max_tokens = min(int(max_tokens or 0), 2200)
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
            "messages": [
                {
                    "role": "user",
                    "content": prompt,
                }
            ],
            "temperature": 0.1,
            "max_tokens": max_tokens,
            # All nVentures LLM prompts request JSON. MiniMax M3 supports
            # structured JSON output through OpenRouter, which is safer than
            # relying on free-form text being returned in message.content.
            "response_format": {"type": "json_object"},
        }

        log(f"      OpenRouter request: {model}")

        try:
            response = http_request_with_retry(
                "POST",
                OPENROUTER_URL,
                headers=headers,
                json_body=body,
                timeout=timeout,
                max_attempts=2,
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
                    log(
                        f"      {model}: blocked by data policy - stopping."
                    )
                    raise RuntimeError(DATA_POLICY_HELP)

                error_text = f"{model}: {code} - {message}"
                errors.append(error_text)

                log(f"      OpenRouter error: {error_text}")

                # This is account-wide, not model-specific. Trying another
                # model immediately only creates another request against the
                # same exhausted in-flight budget.
                if code == 402 and "in_flight_budget_exhausted" in str(message):
                    log(
                        "      OpenRouter in-flight budget exhausted; "
                        "not trying another model."
                    )
                    break

                log(f"      Trying next model...")
                continue

            choices = data.get("choices", [])

            if not choices:
                error_text = f"{model}: no choices returned"
                errors.append(error_text)
                log(f"      {error_text}")
                log("      Trying next model...")
                continue

            message = choices[0].get("message", {}) or {}
            content = message.get("content", "")

            # Some OpenRouter providers return structured content blocks
            # instead of a plain string. Normalize those into text.
            if isinstance(content, list):
                parts = []
                for item in content:
                    if isinstance(item, str):
                        parts.append(item)
                    elif isinstance(item, dict):
                        part_text = item.get("text") or item.get("content")
                        if part_text:
                            parts.append(str(part_text))
                content = "\n".join(parts).strip()

            if isinstance(content, dict):
                content = content.get("text") or content.get("content") or ""

            content = str(content).strip() if content is not None else ""

            if not content:
                finish_reason = choices[0].get("finish_reason", "")
                refusal = message.get("refusal", "")
                reasoning = message.get("reasoning", "")

                # Reasoning is diagnostic only. Do not feed it into the JSON
                # parser because it is not the requested final answer.
                error_text = (
                    f"{model}: empty content"
                    f" (finish_reason={finish_reason!r}"
                    f", refusal={bool(refusal)}"
                    f", reasoning_present={bool(reasoning)})"
                )
                errors.append(error_text)
                log(f"      {error_text}")
                log(
                    "      Full response contained no usable final text; "
                    "trying the next model/provider."
                )
                continue

            log(f"      OpenRouter response: {model} successful")

            return content

        except RuntimeError as error:
            if _is_data_policy_error(error):
                log(
                    f"      {model}: blocked by data policy - stopping."
                )
                raise RuntimeError(DATA_POLICY_HELP) from error

            error_text = f"{model}: {error}"
            errors.append(error_text)

            log(f"      OpenRouter request failed: {model}")
            log(f"      {error}")
            log("      Trying next model...")

            continue

        except Exception as error:  # noqa: BLE001
            error_text = f"{model}: {error}"
            errors.append(error_text)

            log(f"      Unexpected OpenRouter failure: {model}")
            log(f"      {error}")
            log("      Trying next model...")

            continue

    error_message = (
        "All OpenRouter models failed.\n"
        + "\n".join(errors)
    )

    log("      ALL OPENROUTER MODELS FAILED")
    log(error_message)

    raise RuntimeError(error_message)


# ============================================================================
# FREE WEB SEARCH (FreeSerp)
# ============================================================================

def freeserp_search(
    query,
    *,
    timeout=120,
    max_results=5,
    include_domains=None,
    log=print,
):
    """Search the web through FreeSerp's free, keyless JSON endpoint."""
    query = normalize_text(query)
    if not query:
        raise ValueError("Search query is empty.")

    try:
        max_results = max(1, min(int(max_results or 5), 20))
    except (TypeError, ValueError):
        max_results = 5

    request_url = (
        f"{FREESEPR_URL}"
        f"?index=web"
        f"&q={quote_plus(query)}"
        f"&size={max_results}"
    )

    response = http_request_with_retry(
        "GET",
        request_url,
        timeout=timeout,
        max_attempts=3,
        label="FreeSerp",
        log=log,
    )

    try:
        data = response.json()
    except ValueError as error:
        raise RuntimeError(
            "FreeSerp returned a non-JSON response:\n"
            + response.text[:1000]
        ) from error

    if isinstance(data, dict) and data.get("error"):
        raise RuntimeError(
            "FreeSerp error:\n" + json.dumps(data["error"], indent=2)
        )

    if not isinstance(data, dict):
        raise RuntimeError("FreeSerp returned an unexpected response format.")

    results = data.get("results", [])
    if not isinstance(results, list):
        results = []

    # Keep compatibility with the old Tavily include_domains argument.
    # Filtering is performed locally so we do not depend on a provider-specific
    # filter parameter.
    if include_domains:
        allowed = {
            normalize_domain(domain)
            for domain in include_domains
            if normalize_domain(domain)
        }
        if allowed:
            filtered = []
            for result in results:
                domain = normalize_domain(result.get("url", ""))
                if domain in allowed or any(
                    domain.endswith("." + root) for root in allowed
                ):
                    filtered.append(result)
            results = filtered

    data["results"] = results
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
        content = normalize_text(
            result.get("content")
            or result.get("summary")
            or result.get("snippet")
            or result.get("description")
            or ""
        )
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


def maturity_flag(record, research_text, max_total_funding=None):
    """Return a warning only for clearly mature/growth-stage evidence."""
    combined = " ".join(
        [
            normalize_text(record.get("Stage", "")),
            normalize_text(record.get("Last Round", "")),
            normalize_text(research_text),
        ]
    )

    combined_lower = combined.lower()
    warnings = []

    for term in MATURE_TERMS:
        snippet = _find_term_match(combined, combined_lower, term)
        if snippet:
            warnings.append(f"mentions '{term}'")

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
# PARTNER DISCOVERY
# ============================================================================

SOUTH_ASIA_COUNTRIES = [
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
]

PARTNER_TYPE_QUERIES = {
    "Venture Capital": "venture capital VC funds",
    "Accelerator": "startup accelerators",
    "Angel Syndicate / Angel Network": "angel syndicates angel networks",
    "Incubator": "startup incubators",
    "Seed Fund": "seed funds",
    "Corporate Venture Capital": "corporate venture capital CVC",
    "Family Office": "family offices startup investors",
}


def partner_discovery_prompt(research_text, countries, partner_types):
    return (
        "You are an investor-relations research analyst helping nVentures build "
        "a South Asia co-investor and ecosystem partner database.\n\n"
        "COUNTRIES:\n"
        + ", ".join(countries)
        + "\n\nPARTNER TYPES:\n"
        + ", ".join(partner_types)
        + "\n\n"
        "Use ONLY the supplied web research. Identify real, currently operating "
        "investment/ecosystem organizations whose own base/headquarters is in "
        "one of the requested countries. Do not list portfolio companies, "
        "individual angel investors, media companies, directories, or generic "
        "service providers as partners.\n\n"
        "A partner can be a VC, accelerator, angel syndicate/network, incubator, "
        "seed fund, CVC, or family office that actively works with startups.\n\n"
        "Return ONLY valid JSON in this exact shape:\n"
        "{\n"
        '  "partners": [\n'
        "    {\n"
        '      "name": "",\n'
        '      "type": "",\n'
        '      "country": "",\n'
        '      "city": "",\n'
        '      "website": "",\n'
        '      "linkedin": "",\n'
        '      "investment_focus": "",\n'
        '      "stage_focus": "",\n'
        '      "typical_check": "",\n'
        '      "portfolio_examples": "",\n'
        '      "recent_activity": "",\n'
        '      "reason_relevant": "",\n'
        '      "confidence": "high"\n'
        "    }\n"
        "  ]\n"
        "}\n\n"
        "Important:\n"
        "- Prefer the organization's official website when available.\n"
        "- Do not invent a website, LinkedIn URL, check size, portfolio, or "
        "investment stage.\n"
        '- Missing information must be "".\n'
        '- Confidence must be high, medium, or low.\n\n'
        "WEB RESEARCH:\n"
        + research_text
    )


def _partner_record_key(name):
    return normalize_company_name(name)


def build_existing_partner_indexes(worksheet):
    values = worksheet.get_all_values()
    indexes = {"names": set(), "domains": set(), "linkedin": set()}

    if not values:
        return indexes

    headers = values[0]
    first_idx, _ = build_header_index(headers)

    name_col = (
        first_idx.get(normalize_header("Company Name"))
        or first_idx.get(normalize_header("Partner Name"))
        or first_idx.get(normalize_header("Name"))
    )
    website_col = (
        first_idx.get(normalize_header("Company Portfolio"))
        or first_idx.get(normalize_header("Portfolio"))
        or first_idx.get(normalize_header("Website"))
        or first_idx.get(normalize_header("Company Website"))
    )
    linkedin_col = (
        first_idx.get(normalize_header("Company LinkedIn"))
        or first_idx.get(normalize_header("LinkedIn"))
        or first_idx.get(normalize_header("LinkedIn URL"))
    )

    for row in values[1:]:
        if name_col and len(row) >= name_col:
            key = _partner_record_key(row[name_col - 1])
            if key:
                indexes["names"].add(key)

        if website_col and len(row) >= website_col:
            domain = normalize_domain(row[website_col - 1])
            if domain:
                indexes["domains"].add(domain)

        if linkedin_col and len(row) >= linkedin_col:
            linkedin = normalize_linkedin(row[linkedin_col - 1])
            if linkedin:
                indexes["linkedin"].add(linkedin)

    return indexes


def partner_already_exists(record, indexes):
    name = _partner_record_key(record.get("name", ""))
    domain = normalize_domain(record.get("website", ""))
    linkedin = normalize_linkedin(record.get("linkedin", ""))

    return (
        (name and name in indexes["names"])
        or (domain and domain in indexes["domains"])
        or (linkedin and linkedin in indexes["linkedin"])
    )


def build_partner_sheet_row(headers, record):
    row = [""] * len(headers)
    first_idx, _ = build_header_index(headers)

    aliases = {
        "name": ["Company Name", "Partner Name", "Name"],
        "website": [
            "Company Portfolio",
            "Portfolio",
            "Portfolio URL",
            "Website",
            "Company Website",
        ],
        "linkedin": ["Company LinkedIn", "LinkedIn", "LinkedIn URL"],
        "type": ["Type", "Partner Type", "Investor Type", "Category"],
        "country": ["Country", "HQ Country", "Headquarters Country"],
        "city": ["City", "Headquarters", "HQ"],
        "investment_focus": [
            "Investment Focus",
            "Sector Focus",
            "Focus",
            "Investment Thesis",
        ],
        "stage_focus": ["Stage", "Stage Focus", "Investment Stage"],
        "typical_check": ["Typical Check", "Check Size", "Ticket Size"],
        "portfolio_examples": [
            "Portfolio Examples",
            "Portfolio Companies",
            "Notable Portfolio",
        ],
        "recent_activity": ["Recent Activity", "Recent Investments", "Activity"],
        "reason_relevant": [
            "nVentures Relevance",
            "Relevance",
            "Reason",
            "Notes",
        ],
    }

    for key, possible_headers in aliases.items():
        value = normalize_text(record.get(key, ""))
        if not value:
            continue

        for header in possible_headers:
            col = first_idx.get(normalize_header(header))
            if col:
                row[col - 1] = value
                break

    # Existing sourcing engine expects a usable portfolio field. If the
    # partner sheet has that column, the official website is a better default
    # than leaving it blank.
    return row


def discover_partners(
    *,
    openrouter_api_key,
    tavily_api_key=None,
    openrouter_model="openrouter/free",
    countries=None,
    partner_types=None,
    max_per_search=12,
    tavily_timeout=120,
    openrouter_timeout=120,
    max_tavily_results=8,
    max_research_chars=12000,
    request_delay=2.0,
    progress_callback=None,
    log_callback=None,
):
    """Discover South Asia investor/ecosystem partners without changing Sheets."""

    countries = countries or SOUTH_ASIA_COUNTRIES
    partner_types = partner_types or list(PARTNER_TYPE_QUERIES)

    countries = [normalize_text(x) for x in countries if normalize_text(x)]
    partner_types = [normalize_text(x) for x in partner_types if normalize_text(x)]

    if not countries:
        raise ValueError("Select at least one country.")
    if not partner_types:
        raise ValueError("Select at least one partner type.")

    log_lines = []

    def log(message=""):
        stamp = datetime.now(timezone.utc).strftime("%H:%M:%S")
        text = f"[{stamp}] {message}"
        log_lines.append(text)
        print(text, flush=True)
        if log_callback:
            try:
                log_callback(text)
            except Exception:
                pass

    models = build_model_order(openrouter_model)

    def llm(prompt):
        return call_llm(
            prompt,
            api_key=openrouter_api_key,
            models=models,
            timeout=openrouter_timeout,
            max_tokens=1400,
            log=log,
        )

    def search(query):
        return freeserp_search(
            query,
            timeout=tavily_timeout,
            max_results=max_tavily_results,
            log=log,
        )

    search_jobs = [
        (country, partner_type)
        for country in countries
        for partner_type in partner_types
    ]

    discovered = []
    seen = {"names": set(), "domains": set(), "linkedin": set()}
    total = max(1, len(search_jobs))

    for index, (country, partner_type) in enumerate(search_jobs, start=1):
        message = f"Searching {country} — {partner_type} ({index}/{total})"
        log(message)
        if progress_callback:
            progress_callback(int((index - 1) * 85 / total), message)

        query = (
            f"best {PARTNER_TYPE_QUERIES.get(partner_type, partner_type)} "
            f"in {country} startup investors portfolio founders "
            f"co-investment"
        )

        try:
            result = search(query)
            research_text = combined_raw_text(
                result, char_limit=max_research_chars
            )

            if not research_text:
                log("  No usable search results.")
                continue

            extraction = safe_json_parse(
                llm(
                    partner_discovery_prompt(
                        research_text, [country], [partner_type]
                    )
                )
            )

            candidates = extraction.get("partners", [])
            if not isinstance(candidates, list):
                candidates = []

            for raw in candidates[:max_per_search]:
                if not isinstance(raw, dict):
                    continue

                record = {
                    "name": normalize_text(raw.get("name", "")),
                    "type": normalize_text(raw.get("type", "")),
                    "country": normalize_text(raw.get("country", "")),
                    "city": normalize_text(raw.get("city", "")),
                    "website": normalize_text(raw.get("website", "")),
                    "linkedin": normalize_text(raw.get("linkedin", "")),
                    "investment_focus": normalize_text(
                        raw.get("investment_focus", "")
                    ),
                    "stage_focus": normalize_text(raw.get("stage_focus", "")),
                    "typical_check": normalize_text(
                        raw.get("typical_check", "")
                    ),
                    "portfolio_examples": normalize_text(
                        raw.get("portfolio_examples", "")
                    ),
                    "recent_activity": normalize_text(
                        raw.get("recent_activity", "")
                    ),
                    "reason_relevant": normalize_text(
                        raw.get("reason_relevant", "")
                    ),
                    "confidence": normalize_text(
                        raw.get("confidence", "")
                    ).lower(),
                }

                if not record["name"]:
                    continue

                name_key = _partner_record_key(record["name"])
                domain = normalize_domain(record["website"])
                linkedin = normalize_linkedin(record["linkedin"])

                if (
                    name_key in seen["names"]
                    or (domain and domain in seen["domains"])
                    or (linkedin and linkedin in seen["linkedin"])
                ):
                    continue

                seen["names"].add(name_key)
                if domain:
                    seen["domains"].add(domain)
                if linkedin:
                    seen["linkedin"].add(linkedin)

                record["source_country"] = country
                record["source_type"] = partner_type
                discovered.append(record)

        except Exception as error:
            log(f"  Search error: {error}")

        if request_delay:
            time.sleep(request_delay)

    if progress_callback:
        progress_callback(100, f"Discovery complete — {len(discovered)} candidates")

    return {
        "partners": discovered,
        "searches": len(search_jobs),
        "log": log_lines,
    }


def add_discovered_partners_to_sheet(
    worksheet,
    records,
    log=print,
):
    """Append selected discovered partners to the existing Partner sheet."""

    headers = worksheet.row_values(1)
    if not headers:
        raise RuntimeError("Partner sheet has no header row.")

    indexes = build_existing_partner_indexes(worksheet)
    added = []
    skipped = []

    for record in records:
        name = normalize_text(record.get("name", ""))
        if not name:
            continue

        if partner_already_exists(record, indexes):
            skipped.append(name)
            log(f"  SKIP partner duplicate: {name}")
            continue

        row = build_partner_sheet_row(headers, record)
        if not any(normalize_text(x) for x in row):
            skipped.append(name)
            log(f"  SKIP partner - no compatible sheet columns: {name}")
            continue

        # Insert at the first row after the actual used range. Do not rely on
        # append_row(), because worksheet dimensions/formatting can make its
        # append position unreliable.
        current_values = worksheet.get_all_values()
        if not current_values:
            raise RuntimeError("Partner sheet returned no rows.")

        last_populated_row = 1
        for row_number, existing_row in enumerate(current_values, start=1):
            if any(normalize_text(value) for value in existing_row):
                last_populated_row = row_number

        insert_at = last_populated_row + 1
        log(f"  INSERTING PARTNER: {name} at row {insert_at}")

        worksheet.insert_row(
            row,
            index=insert_at,
            value_input_option="USER_ENTERED",
        )

        time.sleep(0.75)
        after_values = worksheet.get_all_values()

        if len(after_values) != len(current_values) + 1:
            raise RuntimeError(
                "Partner insert verification failed: unexpected row count. "
                f"Before={len(current_values)}, After={len(after_values)}, "
                f"Expected={len(current_values) + 1}"
            )

        name_col = None
        for col_index, header in enumerate(headers):
            if normalize_header(header) in {"company name", "partner name", "name"}:
                name_col = col_index
                break

        if name_col is None:
            raise RuntimeError("Partner insert verification failed: name column not found.")

        actual_row = after_values[insert_at - 1]
        actual_name = normalize_text(actual_row[name_col]) if len(actual_row) > name_col else ""

        if _partner_record_key(actual_name) != _partner_record_key(name):
            raise RuntimeError(
                "Partner insert verification failed: wrong row was written. "
                f"Expected={name!r}, Found={actual_name!r}, Row={insert_at}"
            )

        added.append(name)

        indexes["names"].add(_partner_record_key(name))
        domain = normalize_domain(record.get("website", ""))
        linkedin = normalize_linkedin(record.get("linkedin", ""))
        if domain:
            indexes["domains"].add(domain)
        if linkedin:
            indexes["linkedin"].add(linkedin)

        log(f"  ADDED PARTNER: {name}")

    return {"added": added, "skipped": skipped}


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
        "   Exclude clearly mature/growth companies.\n\n"
        "2. REAL COMPANY:\n"
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
        "The sourcing mandate does NOT impose a funding ceiling or a specific\n"
        "funding-round requirement. Do NOT reject a company based solely on\n"
        "the amount it has raised or whether it has completed a Series A.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{{\n"
        '  "b2b": true,\n'
        '  "active_company": true,\n'
        '  "too_mature": false,\n'
        '  "confidence": "high",\n'
        '  "reason": "Factual explanation"\n'
        "}}\n\n"
        "Rules:\n\n"
        "B2B:\n"
        "The company must primarily sell to businesses, institutions, or\n"
        "organizations.\n\n"
        "ACTIVE COMPANY:\n"
        "There should be credible evidence that the company is an actual\n"
        "operating business with a real product or service.\n\n"
        "MATURITY:\n"
        "Reject only companies that are clearly mature/growth-stage businesses\n"
        "and are no longer appropriate for an early-stage sourcing pipeline.\n\n"
        "FUNDING:\n"
        "Funding amount is NOT a screening criterion.\n"
        "Do NOT reject because total funding exceeds any particular amount.\n\n"
        "FUNDING STAGE:\n"
        "Funding stage is NOT a screening criterion.\n"
        "Do NOT reject a company merely because it has completed a Series A.\n"
        "Do NOT reject a company merely because it has raised later funding.\n\n"
        "Do NOT treat missing evidence as positive evidence.\n\n"
        'Confidence must be "high", "medium", or "low".\n'
    )
SRI_LANKAN_FOUNDER_DISCOVERY_QUERIES = [
    '"Sri Lankan founder" startup company founder',
    '"Sri Lankan entrepreneur" startup founder company',
    '"Sri Lanka" founder "startup" B2B company',
    '"Sri Lankan" co-founder startup company',
]


def sri_lankan_founder_candidate_prompt(web_text):
    return (
        "You are a venture capital sourcing analyst helping nVentures find "
        "companies founded by Sri Lankan founders.\n\n"
        "IMPORTANT SCOPE:\n"
        "- The company may be headquartered ANYWHERE in the world.\n"
        "- At least one actual founder or co-founder must be Sri Lankan.\n"
        "- Do NOT infer Sri Lankan identity from a person's name, appearance, "
        "location, language, or surname.\n"
        "- Only include a founder when the supplied web evidence explicitly "
        "supports the Sri Lankan connection or clearly identifies the person "
        "as Sri Lankan.\n"
        "- The person must actually be a founder/co-founder of the company, "
        "not merely an employee, investor, advisor, executive, or alumnus.\n"
        "- The company must appear to be a real operating business.\n"
        "- Prefer B2B companies, but do not invent B2B status when the evidence "
        "does not support it.\n\n"
        "Return ONLY valid JSON in this exact shape:\n\n"
        "{\n"
        '  "candidates": [\n'
        "    {\n"
        '      "company_name": "Company Name",\n'
        '      "founder_name": "Founder Name",\n'
        '      "founder_role": "Founder / Co-founder",\n'
        '      "founder_linkedin": "https://linkedin.com/...",\n'
        '      "founder_sri_lankan_evidence": "Short factual evidence.",\n'
        '      "evidence_url": "https://...",\n'
        '      "company_website": "https://...",\n'
        '      "sector_guess": "Sector",\n'
        '      "why_it_fits": "Short factual explanation."\n'
        "    }\n"
        "  ]\n"
        "}\n\n"
        "If the evidence is insufficient, leave that company out.\n\n"
        "WEB CONTENT:\n"
        f"{web_text}\n"
    )


def sri_lankan_founder_verification_prompt(
    company_name,
    founder_name,
    founder_evidence,
    evidence_url,
    deep_text,
):
    return (
        "You are the final verification analyst for an investment sourcing "
        "pipeline.\n\n"
        f"COMPANY: {company_name}\n"
        f"FOUNDER: {founder_name}\n"
        f"FOUNDER EVIDENCE FROM DISCOVERY: {founder_evidence}\n"
        f"EVIDENCE URL: {evidence_url}\n\n"
        "DEEP RESEARCH:\n"
        f"{deep_text}\n\n"
        "Evaluate ONLY using the supplied research. Do not infer nationality "
        "or Sri Lankan identity from a name, surname, location, ethnicity, "
        "or other indirect clues.\n\n"
        "The company qualifies for this special sourcing workflow only if:\n"
        "1. The named person is actually a founder/co-founder of the company.\n"
        "2. The supplied evidence explicitly supports that the founder is "
        "Sri Lankan.\n"
        "3. The company is a real operating business.\n"
        "4. The company primarily sells to businesses, institutions, or "
        "organizations.\n\n"
        "Company geography is NOT a criterion. The company can be based "
        "anywhere in the world.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{\n"
        '  "b2b": true,\n'
        '  "active_company": true,\n'
        '  "founder_is_founder": true,\n'
        '  "founder_is_sri_lankan": true,\n'
        '  "confidence": "high",\n'
        '  "reason": "Short factual explanation.",\n'
        '  "founder_evidence": "Short factual evidence.",\n'
        '  "evidence_url": "https://..."\n'
        "}\n\n"
        'Confidence must be "high", "medium", or "low".\n'
        "If the Sri Lankan-founder criterion cannot be verified from the "
        "research, set founder_is_sri_lankan to false.\n"
    )


def _set_first_matching_field(row, first_idx, aliases, value):
    """Set the first matching sheet column from a list of aliases."""
    value = normalize_text(value)
    if not value:
        return False

    for header in aliases:
        col = get_col(header, first_idx)
        if col:
            row[col - 1] = value
            return True

    return False


def build_sri_lankan_founder_sheet_row(
    headers,
    record,
    founder_name,
    founder_linkedin,
    founder_evidence,
    evidence_url,
    verification,
    source_label="Sri Lankan Founder Sourcing",
):
    """
    Build a normal Active Sourcing row while adding founder-specific evidence
    wherever the existing sheet has matching columns. If dedicated evidence
    columns do not exist, the evidence is preserved in Extra Notes / Reason.
    """
    row, populated = build_sheet_row(
        headers,
        record,
        source_label,
        verification,
        "",
    )

    first_idx, all_idx = build_header_index(headers)

    _set_first_matching_field(
        row,
        first_idx,
        ["Founder Name", "Founder", "Founders"],
        founder_name,
    )
    _set_first_matching_field(
        row,
        first_idx,
        ["Founder LinkedIn", "Founder Linkedin", "Founder LinkedIn URL"],
        founder_linkedin,
    )
    _set_first_matching_field(
        row,
        first_idx,
        [
            "Founder Nationality",
            "Founder Country",
            "Founder Geography",
        ],
        "Sri Lankan",
    )
    _set_first_matching_field(
        row,
        first_idx,
        [
            "Founder Evidence",
            "Sri Lankan Founder Evidence",
            "Founder Verification",
        ],
        founder_evidence,
    )
    _set_first_matching_field(
        row,
        first_idx,
        [
            "Founder Evidence URL",
            "Sri Lankan Founder Evidence URL",
            "Evidence URL",
        ],
        evidence_url,
    )
    _set_first_matching_field(
        row,
        first_idx,
        [
            "Sourcing Type",
            "Source Type",
            "Sourcing Method",
        ],
        source_label,
    )

    # Preserve the founder audit trail even if the sheet has no dedicated
    # evidence columns.
    evidence_note = (
        f"Sri Lankan founder: {founder_name}. "
        f"Evidence: {founder_evidence or 'Verified from supplied research.'}"
    )
    if evidence_url:
        evidence_note += f" Source: {evidence_url}"

    notes_cols = all_idx.get(normalize_header("Extra Notes"), [])
    if notes_cols:
        existing_note = normalize_text(row[notes_cols[0] - 1])
        row[notes_cols[0] - 1] = (
            f"{existing_note} {evidence_note}".strip()
        )

    return row, populated


def portfolio_sri_lankan_founder_candidate_prompt(partner_name, portfolio_text):
    return (
        "You are a venture capital sourcing analyst.\n\n"
        f"PARTNER VC: {partner_name}\n\n"
        "Identify portfolio companies visible in the supplied portfolio research.\n"
        "Do not infer founder nationality from names or locations. This step is "
        "ONLY for identifying genuine portfolio companies; founder nationality "
        "will be verified separately.\n\n"
        "Return ONLY valid JSON:\n\n"
        "{\n"
        '  "candidates": [\n'
        "    {\n"
        '      "company_name": "Company Name",\n'
        '      "company_website": "https://...",\n'
        '      "sector_guess": "Sector",\n'
        '      "why_it_fits": "Short factual explanation."\n'
        "    }\n"
        "  ]\n"
        "}\n\n"
        "WEB CONTENT:\n"
        f"{portfolio_text}\n"
    )


def run_sri_lankan_founder_sourcing(
    *,
    sourcing_ws,
    partner_ws,
    control_ws=None,
    openrouter_api_key,
    tavily_api_key=None,
    openrouter_model="openrouter/free",
    target_companies=25,
    max_partners=12,
    max_deep_research=45,
    max_candidates_per_search=8,
    max_candidates_per_partner=10,
    tavily_timeout=120,
    openrouter_timeout=120,
    max_tavily_results=5,
    max_research_chars=14000,
    request_delay=1.0,
    require_b2b=True,
    progress_callback=None,
    log_callback=None,
):
    """Find globally based companies with verified Sri Lankan founders using
    both open-web discovery and VC portfolio discovery, then merge, verify,
    dedupe, and write accepted companies into Active Sourcing.
    """
    if not openrouter_api_key:
        raise RuntimeError("OPENROUTER_API_KEY is missing.")
    if partner_ws is None:
        raise RuntimeError("Partner Database worksheet is required for VC portfolio discovery.")

    target_companies = int(target_companies)
    max_partners = int(max_partners)
    max_deep_research = int(max_deep_research)
    max_candidates_per_search = int(max_candidates_per_search)
    max_candidates_per_partner = int(max_candidates_per_partner)
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
            except Exception:
                pass

    def progress(value, message=""):
        if message:
            log(message)
        if progress_callback:
            try:
                progress_callback(max(0, min(100, int(value))), message)
            except Exception:
                pass

    models = build_model_order(openrouter_model)

    def llm(prompt, max_tokens=1400):
        return call_llm(
            prompt,
            api_key=openrouter_api_key,
            models=models,
            timeout=openrouter_timeout,
            max_tokens=max_tokens,
            log=log,
        )

    def search(query, max_results=None, include_domains=None):
        return freeserp_search(
            query,
            timeout=tavily_timeout,
            max_results=max_results or max_tavily_results,
            include_domains=include_domains,
            log=log,
        )

    sourcing_headers = sourcing_ws.row_values(1)
    if not sourcing_headers:
        raise RuntimeError("Active Sourcing has no header row.")

    existing_indexes = build_existing_indexes(sourcing_ws)
    run_started = datetime.now(timezone.utc)

    accepted = []
    accepted_details = []
    rejected = []
    duplicates = []
    errors = []
    candidates_considered = 0
    deep_research_count = 0

    # Candidate merge key is company-level, not company+founder. This means
    # a company discovered by both channels is researched/written exactly once.
    merged_candidates = {}

    def add_candidate(candidate, source_label, partner_name=""):
        company_name = normalize_text(candidate.get("company_name", ""))
        if not company_name:
            return False

        key = normalize_company_name(company_name)
        if not key:
            return False

        website = normalize_text(candidate.get("company_website", ""))
        existing = merged_candidates.get(key)
        if existing is None:
            candidate = dict(candidate)
            candidate["company_name"] = company_name
            candidate["company_website"] = website
            candidate["_sources"] = [source_label]
            candidate["_partners"] = [partner_name] if partner_name else []
            merged_candidates[key] = candidate
            return True

        # Preserve the richest founder/company metadata across both channels.
        for field in (
            "founder_name", "founder_role", "founder_linkedin",
            "founder_sri_lankan_evidence", "evidence_url", "company_website",
            "sector_guess", "why_it_fits",
        ):
            if not normalize_text(existing.get(field, "")) and normalize_text(candidate.get(field, "")):
                existing[field] = candidate[field]

        for value in candidate.get("_sources", [source_label]):
            if value and value not in existing["_sources"]:
                existing["_sources"].append(value)
        if source_label not in existing["_sources"]:
            existing["_sources"].append(source_label)
        if partner_name and partner_name not in existing["_partners"]:
            existing["_partners"].append(partner_name)
        return False

    # ------------------------------------------------------------------
    # CHANNEL 1: existing web discovery. Kept intact in spirit and search
    # queries; portfolio discovery is additive, not a replacement.
    # ------------------------------------------------------------------
    for search_index, query in enumerate(SRI_LANKAN_FOUNDER_DISCOVERY_QUERIES):
        progress(
            5 + int(20 * search_index / len(SRI_LANKAN_FOUNDER_DISCOVERY_QUERIES)),
            f"Web founder discovery {search_index + 1}/{len(SRI_LANKAN_FOUNDER_DISCOVERY_QUERIES)}",
        )
        try:
            log(f"[Web Discovery] Searching for Sri Lankan founders: {query}")
            result = search(query)
            web_text = combined_raw_text(result, char_limit=max_research_chars)
            if not web_text:
                log("  No usable search results.")
                continue

            extraction = safe_json_parse(
                llm(sri_lankan_founder_candidate_prompt(web_text), max_tokens=1400)
            )
            candidates = extraction.get("candidates", [])
            if not isinstance(candidates, list):
                candidates = []

            for candidate in candidates[:max_candidates_per_search]:
                if not isinstance(candidate, dict):
                    continue
                company_name = normalize_text(candidate.get("company_name", ""))
                founder_name = normalize_text(candidate.get("founder_name", ""))
                if not company_name or not founder_name:
                    continue
                candidate["_discovery_channel"] = "Web Discovery"
                add_candidate(candidate, "Web Discovery")

            log(f"  Web discovery returned {len(candidates[:max_candidates_per_search])} candidates.")
        except Exception as error:
            log(f"  Web discovery failed: {error}")
            errors.append((query, str(error)))
        time.sleep(request_delay)

    # ------------------------------------------------------------------
    # CHANNEL 2: additive VC portfolio scanning using Partner Database.
    # We use the same partner-selection logic as the normal sourcing engine,
    # but only retain VC/investor-type partners for this special channel.
    # ------------------------------------------------------------------
    try:
        partners = worksheet_rows_as_dicts(partner_ws)
        control_state = get_control_state(control_ws) if control_ws is not None else {}
        selected_partners = choose_partners(
            partners,
            control_state,
            max_partners,
            respect_relevance_flag=False,
        )

        vc_keywords = (
            "vc", "venture", "seed fund", "investment fund", "family office",
            "corporate venture", "cvc", "angel syndicate", "private equity",
        )
        vc_partners = []
        for partner in selected_partners:
            partner_type = normalize_text(
                partner.get("Type", "") or partner.get("Partner Type", "")
            ).lower()
            if not partner_type or any(keyword in partner_type for keyword in vc_keywords):
                vc_partners.append(partner)

        log(f"[VC Portfolio] Selected {len(vc_partners)} VC/investor partners for portfolio scanning.")
        if not vc_partners:
            log("[VC Portfolio] No VC/investor partners available; web discovery will continue on its own.")

        total_partners = max(1, len(vc_partners))
        for partner_index, partner in enumerate(vc_partners, start=1):
            partner_name = normalize_text(partner.get("Company Name", ""))
            if not partner_name:
                continue

            progress(
                25 + int(20 * (partner_index - 1) / total_partners),
                f"VC Portfolio {partner_index}/{total_partners}: {partner_name}",
            )

            try:
                portfolio_url = ""
                for field in (
                    "Company Portfolio", "Portfolio", "Portfolio URL",
                    "Website", "Company Website",
                ):
                    value = normalize_text(partner.get(field, ""))
                    if value:
                        portfolio_url = value
                        break

                portfolio_domain = normalize_domain(portfolio_url)
                if portfolio_domain:
                    log(f"[VC Portfolio] {partner_name} — portfolio domain: {portfolio_domain}")
                    portfolio_result = search(
                        "portfolio companies startups",
                        include_domains=[portfolio_domain],
                    )
                else:
                    log(f"[VC Portfolio] {partner_name} — no portfolio URL; using partner search.")
                    portfolio_result = search(
                        f'"{partner_name}" portfolio companies startups investments'
                    )

                portfolio_text = combined_raw_text(
                    portfolio_result, char_limit=max_research_chars
                )
                if not portfolio_text:
                    log(f"[VC Portfolio] {partner_name} — no usable portfolio research.")
                    continue

                extraction = safe_json_parse(
                    llm(
                        portfolio_sri_lankan_founder_candidate_prompt(
                            partner_name, portfolio_text
                        ),
                        max_tokens=2200,
                    )
                )
                candidates = extraction.get("candidates", [])
                if not isinstance(candidates, list):
                    candidates = []
                candidates = candidates[:max_candidates_per_partner]

                new_count = 0
                for candidate in candidates:
                    if not isinstance(candidate, dict):
                        continue
                    candidate["_discovery_channel"] = "VC Portfolio"
                    if add_candidate(candidate, "VC Portfolio", partner_name):
                        new_count += 1

                log(f"[VC Portfolio] {partner_name} — extracted {len(candidates)} portfolio candidates; {new_count} new after merge.")
            except Exception as error:
                log(f"[VC Portfolio] {partner_name} failed: {error}")
                errors.append((partner_name, str(error)))

            time.sleep(request_delay)
    except Exception as error:
        log(f"[VC Portfolio] Partner Database scan failed: {error}")
        errors.append(("VC Portfolio", str(error)))

    discovery_candidates = list(merged_candidates.values())
    total_candidates = max(1, len(discovery_candidates))
    log(f"Merged discovery pool: {len(discovery_candidates)} unique companies before deep research.")

    # ------------------------------------------------------------------
    # ONE research/verification/write pipeline for both channels.
    # ------------------------------------------------------------------
    for index, candidate in enumerate(discovery_candidates):
        if len(accepted) >= target_companies:
            break
        if deep_research_count >= max_deep_research:
            log("Deep research limit reached.")
            break

        candidates_considered += 1
        company_name = normalize_text(candidate.get("company_name", ""))
        founder_name = normalize_text(candidate.get("founder_name", ""))
        founder_role = normalize_text(candidate.get("founder_role", ""))
        founder_linkedin = normalize_text(candidate.get("founder_linkedin", ""))
        founder_evidence = normalize_text(candidate.get("founder_sri_lankan_evidence", ""))
        evidence_url = normalize_text(candidate.get("evidence_url", ""))
        company_website = normalize_text(candidate.get("company_website", ""))
        sector_guess = normalize_text(candidate.get("sector_guess", ""))
        why_it_fits = normalize_text(candidate.get("why_it_fits", ""))
        sources = candidate.get("_sources", [])
        partners_for_company = candidate.get("_partners", [])

        progress(
            45 + int(50 * index / total_candidates),
            f"Researching {index + 1}/{total_candidates}: {company_name}",
        )
        log(f"Candidate: {company_name}")
        log(f"  Discovery source(s): {', '.join(sources) or 'Unknown'}")
        if partners_for_company:
            log(f"  Partner VC(s): {', '.join(partners_for_company)}")

        if company_is_existing(company_name, website=company_website, indexes=existing_indexes):
            log("  SKIP - company already exists in Google Sheets.")
            duplicates.append(company_name)
            continue

        deep_research_count += 1
        time.sleep(request_delay)

        try:
            research_query = (
                f'"{company_name}" '
                f'{f'"{founder_name}" ' if founder_name else ""}'
                'founder "Sri Lankan" founder company headquarters B2B customers funding investors'
            )
            deep_result = search(research_query)
            deep_text = combined_raw_text(deep_result, char_limit=max_research_chars)
            if not deep_text:
                rejected.append((company_name, "No usable research"))
                log("  REJECT - no usable research.")
                continue

            record = clean_ai_record(
                safe_json_parse(
                    llm(
                        research_prompt(company_name, why_it_fits, deep_text),
                        max_tokens=1400,
                    )
                )
            )

            if not normalize_text(record.get("Company Name", "")):
                record["Company Name"] = company_name
            if not normalize_text(record.get("Sector", "")):
                record["Sector"] = sector_guess
            if not normalize_text(record.get("Website", "")):
                record["Website"] = company_website

            researched_name = normalize_text(record.get("Company Name", "")) or company_name
            website = normalize_text(record.get("Website", ""))
            company_linkedin = normalize_text(record.get("Company LinkedIn", ""))

            # Portfolio candidates may not expose founders on the portfolio page.
            # Use the deep research record as the founder source in that case.
            if not founder_name:
                founder_name = normalize_text(record.get("Founder Name", ""))
            if not founder_linkedin:
                founder_linkedin = normalize_text(record.get("Founder LinkedIn", ""))

            if company_is_existing(
                researched_name,
                website=website,
                linkedin=company_linkedin,
                indexes=existing_indexes,
            ):
                log("  SKIP - duplicate after research.")
                duplicates.append(researched_name)
                continue

            log("  Verifying B2B + active company + Sri Lankan founder...")
            verification = safe_json_parse(
                llm(
                    sri_lankan_founder_verification_prompt(
                        researched_name,
                        founder_name,
                        founder_evidence,
                        evidence_url,
                        deep_text,
                    ),
                    max_tokens=1100,
                )
            )

            b2b = verification.get("b2b", False) is True
            active_company = verification.get("active_company", False) is True
            founder_is_founder = verification.get("founder_is_founder", False) is True
            founder_is_sri_lankan = verification.get("founder_is_sri_lankan", False) is True
            confidence = normalize_text(verification.get("confidence", "")).lower()

            failures = []
            if require_b2b and not b2b:
                failures.append("B2B requirement not verified")
            if not active_company:
                failures.append("Active-company status not verified")
            if not founder_is_founder:
                failures.append("Founder relationship not verified")
            if not founder_is_sri_lankan:
                failures.append("Sri Lankan-founder status not verified")
            if confidence == "low":
                failures.append("Verification confidence is low")

            if failures:
                log("  REJECTED")
                for failure in failures:
                    log(f"    - {failure}")
                rejected.append((researched_name, "; ".join(failures)))
                continue

            verified_evidence = normalize_text(verification.get("founder_evidence", "")) or founder_evidence
            verified_url = normalize_text(verification.get("evidence_url", "")) or evidence_url
            if not verified_evidence:
                verified_evidence = "Verified from supplied research."

            if len(sources) > 1:
                source_label = "Web + VC Portfolio"
            elif sources:
                source_label = sources[0]
            else:
                source_label = "Sri Lankan Founder Sourcing"
            if partners_for_company:
                source_label += " — " + ", ".join(partners_for_company)

            row, populated_fields = build_sri_lankan_founder_sheet_row(
                sourcing_headers,
                record,
                founder_name,
                founder_linkedin,
                verified_evidence,
                verified_url,
                verification,
                source_label=source_label,
            )

            log("  Writing company to Google Sheets...")
            written_row_number = append_and_verify_row(
                sourcing_ws, row, sourcing_headers, researched_name, log=log
            )
            log(f"  CONFIRMED IN GOOGLE SHEETS - row {written_row_number}")

            add_company_to_indexes(
                researched_name,
                website=website,
                linkedin=company_linkedin,
                indexes=existing_indexes,
            )

            accepted.append(researched_name)
            accepted_details.append(
                {
                    "company": researched_name,
                    "founder": founder_name,
                    "founder_role": founder_role,
                    "founder_evidence": verified_evidence,
                    "evidence_url": verified_url,
                    "country": record.get("Country", ""),
                    "headquarters": record.get("Headquarters", ""),
                    "sector": record.get("Sector", ""),
                    "stage": record.get("Stage", ""),
                    "source": source_label,
                    "partner_vcs": partners_for_company,
                    "row": written_row_number,
                    "fields": len(populated_fields),
                }
            )

            progress(95, f"Added {len(accepted)}/{target_companies}: {researched_name}")
        except Exception as error:
            log(f"  COMPANY ERROR - {company_name}: {error}")
            errors.append((company_name, str(error)))
            continue

        time.sleep(request_delay)

    run_finished = datetime.now(timezone.utc)
    progress(100, f"Sri Lankan founder sourcing complete - {len(accepted)}/{target_companies} added.")

    return {
        "started_at": run_started.isoformat(),
        "finished_at": run_finished.isoformat(),
        "target": target_companies,
        "accepted": accepted,
        "accepted_details": accepted_details,
        "rejected": rejected,
        "duplicates": duplicates,
        "partner_errors": errors,
        "deep_research_calls": deep_research_count,
        "candidates_considered": candidates_considered,
        "discovery_candidates": len(discovery_candidates),
        "web_discovery_candidates": sum(1 for c in discovery_candidates if "Web Discovery" in c.get("_sources", [])),
        "vc_portfolio_candidates": sum(1 for c in discovery_candidates if "VC Portfolio" in c.get("_sources", [])),
        "partners_processed": len(vc_partners) if 'vc_partners' in locals() else 0,
        "partner_breakdown": {},
        "log": log_lines,
        "sourcing_mode": "Sri Lankan Founder Sourcing — Web + VC Portfolio",
    }


# ============================================================================
# MAIN ENTRY POINT
# ============================================================================

def run_sourcing(
    *,
    sourcing_ws,
    partner_ws,
    control_ws,
    openrouter_api_key,
    tavily_api_key=None,
    openrouter_model="openrouter/free",
    target_companies=25,
    max_partners=12,
    max_candidates_per_partner=10,
    max_deep_research=45,
    max_total_funding=3_000_000,
    max_team_size_warning=30,
    tavily_timeout=120,
    openrouter_timeout=120,
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

    target_companies = int(target_companies)
    max_partners = int(max_partners)
    max_candidates_per_partner = int(max_candidates_per_partner)
    max_deep_research = int(max_deep_research)
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
        return freeserp_search(
            query,
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
                    max_tokens=1400,
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
                            max_tokens=1400,
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

                # --- AI verification ---
                log("  Running final qualification...")

                verification = safe_json_parse(
                    llm(
                        verification_prompt(researched_name, deep_text),
                        max_tokens=900,
                    )
                )

                b2b = verification.get("b2b", False) is True
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
