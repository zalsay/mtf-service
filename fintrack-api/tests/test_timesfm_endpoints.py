import os
import json
import time
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError

def http_post_json(url, data, headers=None, timeout=30):
    body = json.dumps(data).encode("utf-8")
    h = {"Content-Type": "application/json"}
    if headers:
        h.update(headers)
    req = Request(url, data=body, headers=h, method="POST")
    try:
        with urlopen(req, timeout=timeout) as resp:
            status = resp.getcode()
            txt = resp.read().decode("utf-8")
            try:
                return status, json.loads(txt), txt
            except Exception:
                return status, None, txt
    except HTTPError as e:
        txt = e.read().decode("utf-8")
        try:
            return e.code, json.loads(txt), txt
        except Exception:
            return e.code, None, txt
    except URLError as e:
        return 0, None, str(e)

def register(base_url, email, password, username):
    url = base_url + "/api/v1/auth/register"
    return http_post_json(url, {"email": email, "password": password, "username": username})

def login(base_url, email, password):
    url = base_url + "/api/v1/auth/login"
    return http_post_json(url, {"email": email, "password": password})

def test_predict(base_url):
    url = base_url + "/predict_for_best"
    payload = {"stock_code": "sh510300", "stock_type": 2, "horizon_len": 7, "context_len": 4096, "timesfm_version": "2.5", "user_id": 1}
    status, js, raw = http_post_json(url, payload)
    print("predict status:", status)
    print("predict body:", js if js is not None else raw)
    return status, js, raw

def test_backtest(base_url, token):
    url = base_url + "/api/v1/timesfm/backtest"
    payload = {"symbol": "sh510300", "stock_type": 2, "horizon_len": 7, "context_len": 4096, "buy_threshold_pct": 3.0, "sell_threshold_pct": -1.0, "trade_fee_rate": 0.006}
    headers = {"Authorization": "Bearer " + token}
    status, js, raw = http_post_json(url, payload, headers=headers)
    print("backtest status:", status)
    print("backtest body:", js if js is not None else raw)
    return status, js, raw

def main():
    base_url = os.environ.get("FIN_API_URL", "http://go-api.meetlife.com.cn:9000")
    predict_base_url = os.environ.get("PREDICT_API_URL", "http://office.pardmind.top:58888")
    email = "zalsay@qq.com"
    password = "Cqlzy@1277"

    s2, js2, _ = login(base_url, email, password)
    token = js2["token"] if s2 == 200 and js2 and "token" in js2 else ""
    test_predict(predict_base_url)
    

if __name__ == "__main__":
    main()
