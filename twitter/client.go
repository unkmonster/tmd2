package twitter

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gookit/color"
	"github.com/tidwall/gjson"
	"github.com/unkmonster/tmd2/internal/utils"
)

const bearer = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

var clientScreenNames map[*resty.Client]string = make(map[*resty.Client]string)
var clientBlockStates map[*resty.Client]*atomic.Bool = make(map[*resty.Client]*atomic.Bool)

func Login(authToken string, ct0 string) (*resty.Client, string, error) {
	client := resty.New()
	client.SetAuthToken(bearer)
	client.SetCookie(&http.Cookie{
		Name:  "auth_token",
		Value: authToken,
	})
	client.SetCookie(&http.Cookie{
		Name:  "ct0",
		Value: ct0,
	})
	client.SetHeader("X-Csrf-Token", ct0)

	client.SetRetryCount(5)
	client.AddRetryCondition(func(r *resty.Response, err error) bool {
		return !strings.HasSuffix(r.Request.RawRequest.Host, "twimg.com") && err != nil
	})
	client.SetTransport(&http.Transport{
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   1000,            // 每个主机最大并发连接数
		IdleConnTimeout:       5 * time.Second, // 连接空闲 n 秒后断开它
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		Proxy:                 http.ProxyFromEnvironment,
	})
	SetClientLog(client, LevelNull, io.Discard)

	// 验证登录是否有效
	resp, err := client.R().Get("https://api.x.com/1.1/account/settings.json")
	if err != nil {
		return nil, "", err
	}
	if err = utils.CheckRespStatus(resp); err != nil {
		return nil, "", err
	}

	screenName := gjson.GetBytes(resp.Body(), "screen_name").String()
	clientBlockStates[client] = &atomic.Bool{}
	clientScreenNames[client] = screenName
	return client, screenName, nil
}

func GetClientScreenName(client *resty.Client) string {
	return clientScreenNames[client]
}

func GetClientBlockState(client *resty.Client) bool {
	return clientBlockStates[client].Load()
}

type xRateLimit struct {
	ResetTime time.Time
	Remaining int
	Limit     int
	Ready     bool
	Url       string
}

func (rl *xRateLimit) preRequest() {
	if time.Now().After(rl.ResetTime) {
		log.Printf("expired %s\n", rl.Url)
		rl.Ready = false
		return
	}

	if rl.Remaining > rl.Limit/100+1 {
		rl.Remaining--
	} else {
		color.Warn.Printf("[RateLimit] %s Sleep until %s\n", rl.Url, rl.ResetTime)
		time.Sleep(time.Until(rl.ResetTime) + time.Minute) // 多 sleep 1 分钟，防止服务器仍返回一个速率限制过期的响应
		rl.Ready = false
	}
}

// 必须返回 nil 或就绪的 rateLimit，否则死锁
func makeRateLimit(resp *resty.Response) *xRateLimit {
	header := resp.Header()
	limit := header.Get("X-Rate-Limit-Limit")
	if limit == "" {
		return nil // 没有速率限制信息
	}
	remaining := header.Get("X-Rate-Limit-Remaining")
	if remaining == "" {
		return nil // 没有速率限制信息
	}
	resetTime := header.Get("X-Rate-Limit-Reset")
	if resetTime == "" {
		return nil // 没有速率限制信息
	}

	resetTimeNum, err := strconv.ParseInt(resetTime, 10, 64)
	if err != nil {
		return nil
	}
	remainingNum, err := strconv.Atoi(remaining)
	if err != nil {
		return nil
	}
	limitNum, err := strconv.Atoi(limit)
	if err != nil {
		return nil
	}

	u, _ := url.Parse(resp.Request.URL)
	url := filepath.Join(u.Host, u.Path)

	resetTimeTime := time.Unix(resetTimeNum, 0)
	return &xRateLimit{
		ResetTime: resetTimeTime,
		Remaining: remainingNum,
		Limit:     limitNum,
		Ready:     true,
		Url:       url,
	}
}

type rateLimiter struct {
	limits sync.Map
	conds  sync.Map
}

func (rateLimiter *rateLimiter) check(url *url.URL) {
	if !rateLimiter.shouldWork(url) {
		return
	}

	path := url.Path
	cod, _ := rateLimiter.conds.LoadOrStore(path, sync.NewCond(&sync.Mutex{}))
	cond := cod.(*sync.Cond)
	cond.L.Lock()
	defer cond.L.Unlock()

	lim, loaded := rateLimiter.limits.LoadOrStore(path, &xRateLimit{})
	limit := lim.(*xRateLimit)
	if !loaded {
		// 首次遇见某路径时直接请求初始化它，后续请求等待这次请求使 limit 就绪
		// 响应中没有速率限制信息：此键赋空，意味不进行速率限制
		return
	}

	/*
		同一时刻仅允许一个未就绪的请求通过检查，其余在这里阻塞，等待前者将速率限制就绪
		未就绪的情况：
		1. 首次请求
		2. 休眠后，速率限制过期

		响应钩子中必须使此键就绪/赋空/删除键并唤醒一个新请求，否则会死锁
	*/
	for limit != nil && !limit.Ready {
		cond.Wait()
		lim, loaded := rateLimiter.limits.LoadOrStore(path, &xRateLimit{})
		if !loaded {
			// 上个请求失败了，从它身上继承初始化速率限制的重任
			return
		}
		limit = lim.(*xRateLimit)
	}

	// limiter 为 nil 意味着不对此路径做速率限制
	if limit != nil {
		limit.preRequest()
	}
	//fmt.Printf("start req: %s\n", path)
}

// 使非就绪的速率限制信息就绪
func (rateLimiter *rateLimiter) update(resp *resty.Response) {
	if !rateLimiter.shouldWork(resp.RawResponse.Request.URL) {
		return
	}

	path := resp.RawResponse.Request.URL.Path

	co, _ := rateLimiter.conds.Load(path)
	cond := co.(*sync.Cond)
	cond.L.Lock()
	defer cond.L.Unlock()

	lim, _ := rateLimiter.limits.Load(path)
	if lim == nil {
		// 保险起见，虽然我觉得不会有这种情况发生
		color.Debug.Tips("[ratelimiter:update] load limit fail")
		return
	}
	limit := lim.(*xRateLimit)
	if limit == nil || limit.Ready {
		return
	}

	// 重置速率限制
	newLimiter := makeRateLimit(resp)
	rateLimiter.limits.Store(path, newLimiter)
	cond.Broadcast()
}

// 重置非就绪的速率限制，让其重新初始化
func (rateLimiter *rateLimiter) reset(url *url.URL, resp *resty.Response) {
	if !rateLimiter.shouldWork(url) {
		return
	}

	path := url.Path
	co, ok := rateLimiter.conds.Load(path)
	if !ok {
		return // BeforeRequest 未调用的情况下调用了 OnError
	}
	cond := co.(*sync.Cond)
	cond.L.Lock()
	defer cond.L.Unlock()

	lim, ok := rateLimiter.limits.Load(path)
	if !ok {
		color.Debug.Println("[ratelimiter:reset] load limit fail")
		return
	}
	limiter := lim.(*xRateLimit)
	if limiter == nil {
		return
	}
	if limiter.Ready && resp == nil {
		// 这次请求未能成功发起，不消耗余量
		limiter.Remaining++
		return
	}

	// 将此路径设为首次请求前的状态
	if !limiter.Ready {
		rateLimiter.limits.Delete(path)
		cond.Signal()
	}
}

func (*rateLimiter) shouldWork(url *url.URL) bool {
	return !strings.HasSuffix(url.Host, "twimg.com")
}

func EnableRateLimit(client *resty.Client) {
	rateLimiter := rateLimiter{}

	client.OnBeforeRequest(func(c *resty.Client, req *resty.Request) error {
		u, err := url.Parse(req.URL)
		if err != nil {
			return err
		}
		// temp
		clientBlockStates[c].Store(true)
		rateLimiter.check(u)
		clientBlockStates[c].Store(false)
		return nil
	})

	client.OnSuccess(func(c *resty.Client, resp *resty.Response) {
		rateLimiter.update(resp)
	})

	client.OnError(func(req *resty.Request, err error) {
		var resp *resty.Response = nil
		if v, ok := err.(*resty.ResponseError); ok {
			// Do something with v.Response
			resp = v.Response
		}
		rateLimiter.reset(req.RawRequest.URL, resp)
	})
}

const (
	LevelDebug = iota
	LevelWarn
	LevelError
	LevelNull
)

type stdlog struct {
	level int
	raw   *log.Logger
}

func (log stdlog) Errorf(format string, v ...interface{}) {
	if LevelError >= log.level {
		log.raw.Printf(format, v...)
	}
}
func (log stdlog) Warnf(format string, v ...interface{}) {
	if LevelWarn >= log.level {
		log.raw.Printf(format, v...)
	}
}
func (log stdlog) Debugf(format string, v ...interface{}) {
	if LevelDebug >= log.level {
		log.raw.Printf(format, v...)
	}
}

func SetClientLog(client *resty.Client, level int, out io.Writer) {
	logger := log.New(out, "", log.Ldate|log.Lmicroseconds)
	client.SetLogger(stdlog{level: level, raw: logger})
}
