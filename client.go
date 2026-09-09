package fusionsolar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	ort "github.com/yalue/onnxruntime_go"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"

type CaptchaSolver interface{ SolveCaptcha([]byte) (string, error) }

// ONNXCaptchaSolver implements the captcha solver used by FusionSolarPy.
// The model must expose an image input and a character-logit output.  Output
// is laid out as [characters][alphabet] (the usual FusionSolar model format).
type ONNXCaptchaSolver struct {
	session                   *ort.AdvancedSession
	input                     *ort.Tensor[float32]
	output                    *ort.Tensor[float32]
	width, height, characters int
	alphabet                  string
}

func NewONNXCaptchaSolver(modelPath string, characters int) (*ONNXCaptchaSolver, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, err
	}
	if characters <= 0 {
		characters = 6
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, err
	}
	in, err := ort.NewTensor(ort.NewShape(1, 1, 50, 200), make([]float32, 50*200))
	if err != nil {
		return nil, err
	}
	out, err := ort.NewTensor(ort.NewShape(1, int64(characters), 36), make([]float32, characters*36))
	if err != nil {
		in.Destroy()
		return nil, err
	}
	s, err := ort.NewAdvancedSession(modelPath, []string{"input"}, []string{"output"}, []ort.Value{in}, []ort.Value{out})
	if err != nil {
		in.Destroy()
		out.Destroy()
		return nil, err
	}
	return &ONNXCaptchaSolver{session: s, input: in, output: out, width: 200, height: 50, characters: characters, alphabet: "0123456789abcdefghijklmnopqrstuvwxyz"}, nil
}

func (s *ONNXCaptchaSolver) Close() error {
	if s == nil {
		return nil
	}
	if s.session != nil {
		s.session.Destroy()
	}
	if s.input != nil {
		s.input.Destroy()
	}
	if s.output != nil {
		s.output.Destroy()
	}
	return nil
}

func (s *ONNXCaptchaSolver) SolveCaptcha(data []byte) (string, error) {
	if s == nil || s.session == nil {
		return "", errors.New("captcha ONNX session is not initialized")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	p := s.input.GetData()
	b := img.Bounds()
	for y := 0; y < s.height; y++ {
		for x := 0; x < s.width; x++ {
			r, g, bl, _ := img.At(b.Min.X+x*b.Dx()/s.width, b.Min.Y+y*b.Dy()/s.height).RGBA()
			p[y*s.width+x] = float32((0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 65535)
		}
	}
	if err := s.session.Run(); err != nil {
		return "", err
	}
	v := s.output.GetData()
	var out strings.Builder
	for i := 0; i < s.characters; i++ {
		best := 0
		for j := 1; j < len(s.alphabet); j++ {
			if v[i*len(s.alphabet)+j] > v[i*len(s.alphabet)+best] {
				best = j
			}
		}
		out.WriteByte(s.alphabet[best])
	}
	return out.String(), nil
}

type Client struct {
	Username, Password, HuaweiSubdomain string
	HTTP                                *http.Client
	companyID                           string
	captchaCode                         string
	Solver                              CaptchaSolver
}

func NewClient(username, password, subdomain string, cookies map[string]string, solver CaptchaSolver) (*Client, error) {
	if subdomain == "" {
		subdomain = "uni003eu5"
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	c := &Client{Username: username, Password: password, HuaweiSubdomain: subdomain, HTTP: hc, Solver: solver}
	if cookies != nil {
		u := c.base()
		cs := []*http.Cookie{}
		for k, v := range cookies {
			cs = append(cs, &http.Cookie{Name: k, Value: v})
		}
		hc.Jar.SetCookies(u, cs)
	} else if err := c.configureSession(); err != nil {
		return nil, err
	}
	return c, nil
}
func (c *Client) base() *url.URL {
	u, _ := url.Parse("https://" + c.HuaweiSubdomain + ".fusionsolar.huawei.com")
	return u
}
func (c *Client) loginSubdomain() string {
	s := c.HuaweiSubdomain
	if strings.HasPrefix(s, "region") {
		return s[8:]
	}
	if strings.HasPrefix(s, "uni") {
		return s[6:]
	}
	return s
}
func (c *Client) endpoint(path string) string {
	return "https://" + c.HuaweiSubdomain + ".fusionsolar.huawei.com" + path
}
func (c *Client) loginEndpoint(path string) string {
	return "https://" + c.loginSubdomain() + ".fusionsolar.huawei.com" + path
}
func (c *Client) request(method, rawurl string, query url.Values, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, rawurl, r)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}
	return c.HTTP.Do(req)
}
func decode(resp *http.Response, v any) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpErr(resp.StatusCode, string(b))
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	return nil
}
func nowMS() int64 { return time.Now().UnixMilli() }
func dayStartMS() int64 {
	t := time.Now()
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).UnixMilli()
}
func (c *Client) configureSession() error {
	if err := c.login(true); err != nil {
		return err
	}
	p, err := c.keepAlive()
	if err != nil || p == "" {
		return fusionErr("Login failed. No payload received from keep-alive.")
	}
	_ = p
	q := url.Values{"_": {strconv.FormatInt(nowMS(), 10)}}
	var x struct {
		Data struct {
			MoDN string `json:"moDn"`
		} `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/neteco/web/organization/v2/company/current"), q, nil)
	if e != nil {
		return e
	}
	if r.StatusCode == 400 || r.StatusCode == 500 {
		var d map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &d)
		if d["exceptionId"] == "Query company failed." || d["exceptionId"] == "bad status" {
			return authErr("Invalid response received. Please check the correct Huawei subdomain.")
		}
		return httpErr(r.StatusCode, string(b))
	}
	if err := decode(r, &x); err != nil {
		return authErr("Failed to login into FusionSolarAPI.")
	}
	if x.Data.MoDN == "" {
		return authErr("Failed to login into FusionSolarAPI.")
	}
	c.companyID = x.Data.MoDN
	return nil
}
func (c *Client) login(allowCaptcha bool) error {
	var key PublicKeyData
	r, e := c.request("GET", "https://eu5.fusionsolar.huawei.com/unisso/pubkey", nil, nil)
	if e != nil {
		return e
	}
	if err := decode(r, &key); err != nil {
		return fusionErr("Failed to retrieve public key.")
	}
	password := c.Password
	q := url.Values{}
	endpoint := c.loginEndpoint("/unisso/v2/validateUser.action")
	if key.EnableEncrypt {
		endpoint = c.loginEndpoint("/unisso/v3/validateUser.action")
		q.Set("timeStamp", fmt.Sprint(key.TimeStamp))
		nonce, e := SecureRandomHex()
		if e != nil {
			return e
		}
		q.Set("nonce", nonce)
		password, e = EncryptPassword(key, password)
		if e != nil {
			return e
		}
	} else {
		q.Set("decision", "1")
		q.Set("service", c.endpoint("/unisess/v1/auth?service=/netecowebext/home/index.html#/LOGIN"))
	}
	payload := map[string]any{"organizationName": "", "username": c.Username, "password": password}
	if c.captchaCode != "" {
		payload["verifycode"] = c.captchaCode
		c.captchaCode = ""
	}
	r, e = c.request("POST", endpoint, q, payload)
	if e != nil {
		return e
	}
	var lr map[string]any
	if err := decode(r, &lr); err != nil {
		return fusionErr("Failed to process login response")
	}
	if fmt.Sprint(lr["errorCode"]) == "470" {
		if a, ok := lr["respMultiRegionName"].([]any); ok && len(a) > 1 {
			target := c.loginEndpoint(fmt.Sprint(a[1]))
			rr, e := c.request("GET", target, nil, nil)
			if e != nil {
				return e
			}
			if err := decode(rr, nil); err != nil {
				return err
			}
		}
	}
	msg := fmt.Sprint(lr["errorMsg"])
	if msg != "" && msg != "<nil>" {
		if allowCaptcha && c.Solver != nil && strings.Contains(strings.ToLower(msg), "incorrect verification code") {
			if err := c.solveCaptcha(); err != nil {
				return err
			}
			return c.login(false)
		}
		return authErr("Failed to login into FusionSolarAPI: " + msg)
	}
	return nil
}
func (c *Client) solveCaptcha() error {
	r, e := c.request("GET", c.loginEndpoint("/"), url.Values{"service": {"%2Funisess%2Fv1%2Fauth%3Fservice%3D%252Fnetecowebext%252Fhome%252Findex.html"}}, nil)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if !bytes.Contains(b, []byte("verificationCodeInput")) {
		return authErr("Login failed: Captcha required but captcha not found.")
	}
	rr, e := c.request("GET", c.loginEndpoint("/unisso/verifycode"), url.Values{"timestamp": {strconv.FormatInt(nowMS(), 10)}}, nil)
	if e != nil {
		return e
	}
	img, _ := io.ReadAll(rr.Body)
	rr.Body.Close()
	code, e := c.Solver.SolveCaptcha(img)
	if e != nil {
		return e
	}
	c.captchaCode = code
	req, _ := http.NewRequest("POST", c.loginEndpoint("/unisso/preValidVerifycode"), strings.NewReader("verifycode="+url.QueryEscape(code)+"&index=0"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	pr, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	pb, _ := io.ReadAll(pr.Body)
	pr.Body.Close()
	if string(pb) != "success" {
		return authErr("Login failed: captcha prevalidverify fail.")
	}
	return nil
}
func (c *Client) IsSessionActive() (bool, error) {
	r, e := c.request("GET", c.endpoint("/rest/dpcloud/auth/v1/is-session-alive"), nil, nil)
	if e != nil {
		return false, e
	}
	var d struct {
		Code int `json:"code"`
	}
	if e = decode(r, &d); e != nil {
		return false, e
	}
	return d.Code == 0, nil
}
func (c *Client) ensureLogin() error {
	ok, e := c.IsSessionActive()
	if e != nil || !ok {
		if e := c.configureSession(); e != nil {
			return e
		}
	}
	return nil
}
func (c *Client) keepAlive() (string, error) {
	r, e := c.request("GET", c.endpoint("/rest/dpcloud/auth/v1/keep-alive"), nil, nil)
	if e != nil {
		return "", e
	}
	var d struct {
		Code    int    `json:"code"`
		Payload string `json:"payload"`
	}
	if e = decode(r, &d); e != nil {
		return "", e
	}
	if d.Code != 0 {
		return "", fusionErr("Failed to set keep alive.")
	}
	return d.Payload, nil
}
func (c *Client) KeepAlive() (string, error) {
	if e := c.ensureLogin(); e != nil {
		return "", e
	}
	return c.keepAlive()
}

func (c *Client) GetCookies() map[string]string {
	out := map[string]string{}
	for _, ck := range c.HTTP.Jar.Cookies(c.base()) {
		out[ck.Name] = ck.Value
	}
	return out
}
func (c *Client) LogOut() error {
	r, e := c.request("GET", c.endpoint("/unisess/v1/logout"), url.Values{"service": {c.endpoint("")}}, nil)
	if e != nil {
		return e
	}
	r.Body.Close()
	return nil
}
func (c *Client) GetPowerStatus() (PowerStatus, error) {
	if e := c.ensureLogin(); e != nil {
		return PowerStatus{}, e
	}
	q := url.Values{"queryTime": {strconv.FormatInt(nowMS(), 10)}, "timeZone": {"1"}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Data struct {
			Current float64 `json:"currentPower"`
			Daily   float64 `json:"dailyEnergy"`
			Cum     float64 `json:"cumulativeEnergy"`
		} `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/station/v1/station/total-real-kpi"), q, nil)
	if e != nil {
		return PowerStatus{}, e
	}
	if e = decode(r, &d); e != nil {
		return PowerStatus{}, e
	}
	return PowerStatus{d.Data.Current, d.Data.Daily, d.Data.Cum}, nil
}
func (c *Client) GetCurrentPlantData(id string) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"stationDn": {id}, "clientTime": {strconv.FormatInt(nowMS(), 10)}, "timeZone": {"1"}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Data map[string]any `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/station/v1/overview/station-real-kpi"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if d.Data == nil {
		return nil, fusionErr("Failed to retrieve plant data.")
	}
	return d.Data, nil
}
func (c *Client) GetStationList() ([]map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	body := map[string]any{"curPage": 1, "pageSize": 10, "gridConnectedTime": "", "queryTime": dayStartMS(), "timeZone": 2, "sortId": "createTime", "sortDir": "DESC", "locale": "en_US"}
	var d struct {
		Success bool `json:"success"`
		Data    struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
	}
	r, e := c.request("POST", c.endpoint("/rest/pvms/web/station/v1/station/station-list"), nil, body)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if !d.Success {
		return nil, fusionErr("Failed to retrieve station list")
	}
	return d.Data.List, nil
}
func (c *Client) GetPlantIDs() ([]string, error) {
	ls, e := c.GetStationList()
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, len(ls))
	for _, x := range ls {
		if v, ok := x["dn"].(string); ok {
			out = append(out, v)
		}
	}
	return out, nil
}
func (c *Client) GetDeviceIDs() ([]Device, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"conditionParams.parentDn": {c.companyID}, "conditionParams.mocTypes": {"20814,20815,20816,20819,20822,50017,60066,60014,60015,23037"}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Data []struct {
			MocTypeName string `json:"mocTypeName"`
			DN          string `json:"dn"`
		} `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/neteco/web/config/device/v1/device-list"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	out := make([]Device, 0, len(d.Data))
	for _, x := range d.Data {
		out = append(out, Device{Type: x.MocTypeName, DeviceDN: x.DN, ID: x.DN})
	}
	return out, nil
}
func (c *Client) GetHistoricalData(signalIDs []string, deviceDN string, t time.Time) (map[string]any, error) {
	return c.deviceHistory(signalIDs, deviceDN, t.UnixMilli())
}
func (c *Client) GetRealTimeData(deviceDN string) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"deviceDn": {deviceDN}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d map[string]any
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/device/v1/device-realtime-data"), q, nil)
	if e != nil {
		return nil, e
	}
	e = decode(r, &d)
	return d, e
}
func (c *Client) GetAlarmData(deviceDN string) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	body := map[string]any{"dataType": "CURRENT", "domainType": "OC_SOLAR", "pageNo": 1, "pageSize": 10, "nativeMeDn": deviceDN}
	var d map[string]any
	r, e := c.request("POST", c.endpoint("/rest/pvms/fm/v1/query"), nil, body)
	if e != nil {
		return nil, e
	}
	e = decode(r, &d)
	return d, e
}
func (c *Client) deviceHistory(ids []string, dn string, ms int64) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"deviceDn": {dn}, "date": {strconv.FormatInt(ms, 10)}, "_": {strconv.FormatInt(nowMS(), 10)}}
	for _, id := range ids {
		q.Add("signalIds", id)
	}
	var d map[string]any
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/device/v1/device-history-data"), q, nil)
	if e != nil {
		return nil, e
	}
	e = decode(r, &d)
	return d, e
}
func (c *Client) GetBatteryIDs(plantID string) ([]string, error) {
	f, e := c.GetPlantFlow(plantID)
	if e != nil {
		return nil, e
	}
	flow, _ := f["data"].(map[string]any)
	flow, _ = flow["flow"].(map[string]any)
	nodes, _ := flow["nodes"].([]any)
	out := []string{}
	for _, n := range nodes {
		m, _ := n.(map[string]any)
		name, _ := m["name"].(string)
		if strings.Contains(name, "energy_store") {
			if ids, ok := m["devIds"].([]any); ok && len(ids) > 0 {
				out = append(out, fmt.Sprint(ids[0]))
			}
		}
	}
	return out, nil
}
func (c *Client) GetBatteryBasicStats(id string) (BatteryStatus, error) {
	x, e := c.GetBatteryStatus(id)
	if e != nil {
		return BatteryStatus{}, e
	}
	if len(x) < 9 {
		return BatteryStatus{}, fusionErr("Invalid battery status")
	}
	num := func(i int) float64 {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(fmt.Sprint(x[i]["realValue"]), "-", "0"), 64)
		return v
	}
	return BatteryStatus{num(8), num(2), fmt.Sprint(x[0]["value"]), fmt.Sprint(x[3]["value"]), num(7), num(4), num(5), num(6)}, nil
}
func (c *Client) GetBatteryDayStats(id string, ids []string, queryTime *int64) (map[string]any, error) {
	if ids == nil {
		ids = []string{"30005", "30007"}
	}
	ms := nowMS()
	if queryTime != nil {
		ms = *queryTime
	}
	d, e := c.deviceHistory(ids, id, ms)
	if e != nil {
		return nil, e
	}
	if data, ok := d["data"].(map[string]any); ok {
		names := map[string]string{"30001": "Charging power", "30002": "Discharging power", "30005": "Charge/Discharge power", "30006": "Battery voltage", "30007": "SOC"}
		for k, n := range names {
			if v, ok := data[k].(map[string]any); ok {
				v["name"] = n
			}
		}
		d["data"] = data
	}
	if d["success"] != true {
		return nil, fusionErr("Failed to retrieve battery day stats for " + id)
	}
	return d["data"].(map[string]any), nil
}
func (c *Client) GetInverterDayStats(id string, ids []string, queryTime *int64) (map[string]any, error) {
	if ids == nil {
		ids = []string{"30007"}
	}
	ms := nowMS()
	if queryTime != nil {
		ms = *queryTime
	}
	d, e := c.deviceHistory(ids, id, ms)
	if e != nil {
		return nil, e
	}
	if d["success"] != true {
		return nil, fusionErr("Failed to retrieve inverter day stats for " + id)
	}
	return d["data"].(map[string]any), nil
}
func (c *Client) GetBatteryModuleStats(id, module string, ids []string) (map[string]any, error) {
	if module == "" {
		module = "1"
	}
	if ids == nil {
		ids = ModuleSignals[module]
	} else {
		allowed := map[string]bool{}
		for _, x := range ModuleSignals[module] {
			allowed[x] = true
		}
		for _, x := range ids {
			if !allowed[x] {
				return nil, fusionErr("One or more unknown signal ids for module " + module)
			}
		}
	}
	q := url.Values{"sigids": {strings.Join(ids, ",")}, "dn": {id}, "moduleId": {module}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/device/v1/query-battery-dc"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if !d.Success {
		return nil, fusionErr("Failed to retrieve battery status for " + id)
	}
	return d.Data, nil
}
func (c *Client) GetBatteryStatus(id string) ([]map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"deviceDn": {id}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Success bool `json:"success"`
		Data    []struct {
			Signals []map[string]any `json:"signals"`
		} `json:"data"`
	}
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/device/v1/device-realtime-data"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if !d.Success || len(d.Data) < 2 {
		return nil, fusionErr("Failed to retrieve battery status for " + id)
	}
	return d.Data[1].Signals, nil
}
func (c *Client) GetPlantFlow(id string) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"stationDn": {id}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d map[string]any
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/station/v1/overview/energy-flow"), q, nil)
	if e != nil {
		return nil, e
	}
	e = decode(r, &d)
	if e != nil {
		return nil, e
	}
	if d["success"] != true {
		return nil, fusionErr("Failed to retrieve plant flow for " + id)
	}
	return d, e
}
func (c *Client) GetPlantStats(id string, queryTime *int64) (map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	ms := dayStartMS()
	if queryTime != nil {
		ms = *queryTime
	}
	q := url.Values{"stationDn": {id}, "timeDim": {"2"}, "queryTime": {strconv.FormatInt(ms, 10)}, "timeZone": {"2"}, "timeZoneStr": {"Europe/Vienna"}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d map[string]any
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/station/v1/overview/energy-balance"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if d["success"] != true {
		return nil, fusionErr("Failed to retrieve plant status for " + id)
	}
	return d["data"].(map[string]any), nil
}
func GetLastPlantData(data map[string]any) map[string]any {
	out := map[string]any{}
	axis, _ := data["xAxis"].([]any)
	for k, v := range data {
		if k == "xAxis" || k == "stationTimezone" || k == "clientTimezone" || k == "stationDn" {
			continue
		}
		switch x := v.(type) {
		case []any:
			var last any
			idx := -1
			for i, z := range x {
				if fmt.Sprint(z) != "--" {
					last = z
					idx = i
				}
			}
			if idx >= 0 && idx < len(axis) {
				f, _ := strconv.ParseFloat(fmt.Sprint(last), 64)
				out[k] = map[string]any{"time": axis[idx], "value": f}
			} else {
				out[k] = map[string]any{"time": time.Now().Format("2006-01-02 15:04"), "value": nil}
			}
		case bool:
			out[k] = x
		case string:
			if x == "--" {
				out[k] = nil
			} else if strings.HasPrefix(k, "exist") {
				out[k] = x != "false" && x != "0"
			} else {
				f, e := strconv.ParseFloat(x, 64)
				if e == nil {
					out[k] = f
				} else {
					out[k] = nil
				}
			}
		default:
			out[k] = x
		}
	}
	return out
}
func (c *Client) GetOptimizerStats(id string) ([]map[string]any, error) {
	if e := c.ensureLogin(); e != nil {
		return nil, e
	}
	q := url.Values{"inverterDn": {id}, "_": {strconv.FormatInt(nowMS(), 10)}}
	var d struct {
		Success       bool             `json:"success"`
		Data          []map[string]any `json:"data"`
		ExceptionType any              `json:"exceptionType"`
	}
	r, e := c.request("GET", c.endpoint("/rest/pvms/web/station/v1/layout/optimizer-info"), q, nil)
	if e != nil {
		return nil, e
	}
	if e = decode(r, &d); e != nil {
		return nil, e
	}
	if d.ExceptionType != nil || !d.Success {
		return nil, fusionErr("Failed to retrieve optimizer status for " + id)
	}
	return d.Data, nil
}
func (c *Client) ActivePowerControl(setting string) error {
	opts := map[string]int{"No limit": 0, "Zero Export Limitation": 5, "Limited Power Grid (kW)": 6, "Limited Power Grid (%)": 7}
	v, ok := opts[setting]
	if !ok {
		return fusionErr("Unknown power setting")
	}
	devs, e := c.GetDeviceIDs()
	if e != nil {
		return e
	}
	var dongle string
	for _, d := range devs {
		if d.Type == "Dongle" {
			dongle = d.DeviceDN
			break
		}
	}
	if dongle == "" {
		return fusionErr("Dongle not found")
	}
	body := url.Values{"dn": {dongle}, "changeValues": {fmt.Sprintf(`[{"id":"230190032","value":"%d"}]`, v)}}
	req, e := http.NewRequest("POST", c.endpoint("/rest/pvms/web/device/v1/deviceExt/set-config-signals"), strings.NewReader(body.Encode()))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	r, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	return decode(r, nil)
}
