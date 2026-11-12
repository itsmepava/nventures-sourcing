import math
import re
from urllib.parse import urlsplit

COUNTRIES = ('Afghanistan', 'Bangladesh', 'Bhutan', 'India', 'Maldives', 'Nepal', 'Pakistan', 'Sri Lanka', 'Singapore')
EARLY_STAGES = {'bootstrapped', 'pre-seed', 'seed', 'angel'}

def hq_excerpt_supports(quote, country):
    for match in re.finditer(r'(?:headquartered|headquarters|\bhq|\bbased)\b([^.;\n]{0,90})', quote, re.I):
        clause = re.split(r'\b(?:founder|founders|market|markets|operations|office|offices|serving|serves|customers|but|while)\b', match[1], maxsplit=1, flags=re.I)[0]
        if re.search(r'\b' + re.escape(country) + r'\b', clause, re.I):
            return True
    return False

def name_key(value):
    value = re.sub(r'\b(private|pvt|limited|ltd|llp|inc|incorporated|corp|corporation)\b', '', str(value).casefold())
    return re.sub(r'[^\w]', '', value)

def domain_key(url):
    if not url:
        return None
    host = urlsplit(url if '://' in url else 'https://' + url).hostname
    return host.casefold().removeprefix('www.') if host else None

def linkedin_key(url):
    if not url:
        return None
    parsed = urlsplit(url if '://' in url else 'https://' + url)
    if (parsed.hostname or '').lower().removeprefix('www.') != 'linkedin.com' or not parsed.path.startswith('/company/'):
        return None
    return 'linkedin.com' + parsed.path.lower().rstrip('/')

def qualifies(record):
    """Python makes every acceptance decision; absent/ambiguous evidence fails closed."""
    reasons = []
    country = str(record.get('hq_country', '')).strip()
    evidence = record.get('evidence', {})
    def supported(field):
        refs = evidence.get(field, [])
        return isinstance(refs, list) and any(isinstance(x, dict) and x.get('url', '').startswith(('https://', 'http://')) and len(x.get('quote', '').strip()) >= 15 for x in refs)
    if country not in COUNTRIES or not record.get('headquarters') or not supported('headquarters'):
        reasons.append('Eligible headquarters not established with evidence')
    elif not any(hq_excerpt_supports(x.get('quote', ''), country) for x in evidence.get('headquarters', []) if isinstance(x, dict)):
        reasons.append('HQ evidence does not explicitly establish the claimed country')
    if record.get('b2b') is not True or not supported('b2b'):
        reasons.append('B2B business model not verified')
    if record.get('active') is not True or not supported('active'):
        reasons.append('Active operating company not verified')
    stage = str(record.get('stage', '')).strip().lower().replace('pre seed', 'pre-seed')
    if stage not in EARLY_STAGES or not supported('stage'):
        reasons.append('Early stage not verified (bootstrapped / angel / pre-seed / seed only)')
    amount = record.get('total_funding_usd')
    if isinstance(amount, bool) or not isinstance(amount, (int, float)) or not math.isfinite(amount) or amount < 0 or not supported('funding'):
        reasons.append('Total funding in USD not verified')
    elif amount > 3_000_000:
        reasons.append('Total funding exceeds $3,000,000')
    funding_text = ' '.join(x.get('quote', '') for x in evidence.get('funding', []) if isinstance(x, dict))
    excerpt_amounts = []
    for match in re.finditer(r'(?:USD|US\$|\$)\s*([\d,]+(?:\.\d+)?)\s*(billion|million|bn|[mbk])?\b', funding_text, re.I):
        multiplier = {'billion': 1e9, 'bn': 1e9, 'b': 1e9, 'million': 1e6, 'm': 1e6, 'k': 1e3}.get((match[2] or '').lower(), 1)
        excerpt_amounts.append(float(match[1].replace(',', '')) * multiplier)
        if excerpt_amounts[-1] > 3_000_000:
            reasons.append('Independent funding excerpt exceeds ceiling')
    zero_bootstrap = amount == 0 and re.search(r'no external funding|has not raised|self.funded|bootstrapped', funding_text, re.I)
    if not zero_bootstrap and (not re.search(r'\bUSD\b|US\$|U\.?S\.?\s+dollars', funding_text, re.I) or not re.search(r'\btotal\b|\bcumulative\b|\bto date\b|\bin all\b', funding_text, re.I) or amount not in excerpt_amounts):
        reasons.append('Funding excerpt must explicitly support the total USD amount')
    claims = record.get('funding_claims_usd', [])
    if not isinstance(claims, list):
        reasons.append('Invalid funding evidence')
    elif any(isinstance(x, (int, float)) and not isinstance(x, bool) and x > 3_000_000 for x in claims):
        reasons.append('Conflicting evidence exceeds funding ceiling')
    # Keep the independent maturity veto from the notebook: the model cannot overrule it.
    stage_text = ' '.join(x.get('quote', '') for x in evidence.get('stage', []) if isinstance(x, dict))
    if re.search(r'\bseries\s+[a-z]\b|\bipo\b|\bacquired\b|\bpublicly traded\b', stage_text, re.I):
        reasons.append('Series A+ or mature-company evidence')
    if not str(record.get('name', '')).strip() or not name_key(record.get('name', '')):
        reasons.append('Company name missing')
    return reasons

def duplicate(record, companies):
    n, d, l = name_key(record.get('name', '')), domain_key(record.get('website', '')), linkedin_key(record.get('linkedin', ''))
    return any((n and n == c['name_key']) or (d and d == c.get('domain')) or (l and l == c.get('linkedin_key')) for c in companies)

