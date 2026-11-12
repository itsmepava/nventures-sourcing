"""Exercise the running demo API using only the Python standard library."""
import json
import time
from urllib.request import Request, urlopen
from urllib.error import HTTPError

def request(path, method='GET', body=None, origin=None):
    headers = {'Content-Type': 'application/json'}
    if origin:
        headers['Origin'] = origin
    req = Request('http://127.0.0.1:8000/api' + path, method=method, data=json.dumps(body).encode() if body is not None else None, headers=headers)
    try:
        with urlopen(req, timeout=10) as r:
            raw = r.read()
            return r.status, json.loads(raw) if 'json' in r.headers.get('Content-Type', '') else raw.decode()
    except HTTPError as e:
        return e.code, e.read().decode()

def main():
    status, health = request('/health')
    assert status == 200 and health['mode'] == 'demo', 'Smoke test only runs in demo mode'
    assert request('/dashboard')[0] == 200
    assert request('/partners')[0] == 200
    companies = request('/companies')[1]
    if companies:
        c = companies[0]
        assert request(f'/companies/{c["id"]}')[1]['name'] == c['name']
        assert request('/companies?q=' + c['name'])[1][0]['name'] == c['name']
    assert 'Company Name' in request('/companies/export')[1]
    assert request('/companies/99999999')[0] == 404
    assert request('/runs', 'POST', {'target': 0, 'max_partners': 1})[0] == 422
    assert request('/sync', 'POST')[0] == 400
    assert request('/runs', 'POST', {'target': 1}, 'https://untrusted.example')[0] == 403
    status, run = request('/runs', 'POST', {'target': 100, 'max_partners': 6})
    assert status == 202
    assert request('/runs', 'POST', {'target': 1})[0] == 409
    assert request(f'/runs/{run["id"]}/cancel', 'POST')[0] == 200
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        result = request(f'/runs/{run["id"]}')[1]
        if result['status'] not in ('running', 'queued'):
            assert result['status'] == 'cancelled'
            assert result['events']
            break
        time.sleep(.2)
    else:
        raise AssertionError('Run did not stop in time')
    assert request('/audit')[0] == 200
    print('Localhost API smoke checks passed: reads, filters, profiles, export, validation, demo sync block, origin block, run start, concurrency, cancellation and audit.')

if __name__ == '__main__':
    main()
