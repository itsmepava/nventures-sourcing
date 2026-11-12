from .rules import COUNTRIES

PARTNERS = [('Lanka Angel Network', 'https://www.lankanangelnetwork.com'), ('Venture Catalysts', 'https://venturecatalysts.in'), ('BANSEA', 'https://bansea.org'), ('Indian Angel Network', 'https://www.indianangelnetwork.com'), ('Bangladesh Angels', 'https://bangladeshangels.com'), ('Pakistan Angel Investment Network', '')]

def candidate(index, partner):
    names = ['LedgerLeaf', 'FactorySignal', 'CargoPilot', 'TeamHarbor', 'SupplyNest', 'ClinicRelay', 'InvoiceSpring', 'FieldOrbit', 'ProcurePath', 'WorkMint', 'GridLoom', 'RouteDesk']
    countries = ['Sri Lanka', 'India', 'Singapore', 'Bangladesh', 'Pakistan', 'Nepal']
    name = names[index % len(names)]
    country = countries[index % len(countries)]
    url = f'https://{name.lower()}.example'
    quotes = {'headquarters': f'{name} is headquartered in {country}.', 'b2b': f'{name} sells workflow software to businesses.', 'active': f'{name} operates an active software product with paying customers.', 'stage': f'{name} closed its seed financing round.', 'funding': f'{name} has raised a total of USD {800000 + index * 130000}.'}
    return {'name': name, 'website': url, 'linkedin': '', 'hq_country': country, 'headquarters': country, 'sector': ['Fintech', 'Industrial SaaS', 'Logistics', 'HR technology'][index % 4], 'stage': 'seed', 'b2b': True, 'active': True, 'total_funding_usd': 800000 + index * 130000, 'funding_claims_usd': [800000 + index * 130000], 'description': f'{name} helps businesses simplify complex operations with a connected software platform.', 'product': 'An integrated workflow platform with analytics and automated reporting.', 'target_market': 'Small and medium businesses', 'founders': 'Demo founder', 'founded_year': '2024', 'team_size': str(8 + index), 'customers': 'Demo customer group', 'revenue': 'Not disclosed', 'investors': partner, 'competitors': 'Not researched in demo', 'vision': 'Make business operations simpler.', 'evidence': {k: [{'url': url, 'quote': v}] for k, v in quotes.items()}, 'sources': [{'url': url, 'title': 'Synthetic demo evidence', 'content': ' '.join(quotes.values())}], 'demo': True}

def candidates(partner_index, partner):
    items = [candidate(partner_index * 2, partner), candidate(partner_index * 2 + 1, partner)]
    if partner_index == 0:
        wrong = candidate(11, partner) | {'name': 'Overfunded Demo', 'total_funding_usd': 21_000_000, 'funding_claims_usd': [21_000_000], 'stage': 'series a'}
        items.insert(1, wrong)
        items.append(items[0].copy())
    return items
