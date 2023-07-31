package cninfo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Stock struct {
	Code     string `json:"code"`
	Pinyin   string `json:"pinyin"`
	Category string `json:"category"`
	OrgID    string `json:"orgId"`
	Zwjc     string `json:"zwjc"`
}

type StockListResponse struct {
	StockList []*Stock `json:"stockList"`
}

type Announcement struct {
	SecCode           string `json:"secCode"`
	SecName           string `json:"secName"`
	OrgID             string `json:"orgId"`
	AnnouncementID    string `json:"announcementId"`
	AnnouncementTitle string `json:"announcementTitle"`
	AnnouncementTime  int64  `json:"announcementTime"`
	AdjunctURL        string `json:"adjunctUrl"`
	AdjunctSize       int    `json:"adjunctSize"`
	AdjunctType       string `json:"adjunctType"`
	TileSecName       string `json:"tileSecName"`
	ShortTitle        string `json:"shortTitle"`
}

type HisAnnouncementQueryRequest struct {
	PageNum   int    `json:"pageNum"`
	PageSize  int    `json:"pageSize"`
	Column    string `json:"column"`
	TabName   string `json:"tabName"`
	Plate     string `json:"plate"`
	Stock     string `json:"stock"`
	Searchkey string `json:"searchkey"`
	Secid     string `json:"secid"`
	Category  string `json:"category"`
	Trade     string `json:"trade"`
	SeDate    string `json:"seDate"`
	SortName  string `json:"sortName"`
	SortType  string `json:"sortType"`
	IsHLtitle string `json:"isHLtitle"`
}

type HisAnnouncementQueryResponse struct {
	TotalSecurities   int             `json:"totalSecurities"`
	TotalAnnouncement int             `json:"totalAnnouncement"`
	TotalRecordNum    int             `json:"totalRecordNum"`
	Announcements     []*Announcement `json:"announcements"`
	HasMore           bool            `json:"hasMore"`
	Totalpages        int             `json:"totalpages"`
}

type DividendRecord struct {
	Period         string `json:"F001V"`
	Plan           string `json:"F007V"`
	RecordDate     string `json:"F018D"`
	ExDividendDate string `json:"F020D"`
	PayDate        string `json:"F023D"`
}

type HisDividendResponse struct {
	Path string `json:"path"`
	Code any    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total      int               `json:"total"`
		Count      int               `json:"count"`
		ResultMsg  string            `json:"resultMsg"`
		ResultCode string            `json:"resultCode"`
		Records    []*DividendRecord `json:"records"`
	} `json:"data"`
}

func (r *HisDividendResponse) GetCodeString() string {
	return fmt.Sprintf("%v", r.Code)
}

func (q *HisAnnouncementQueryRequest) FormURLEncoded() string {
	v := *q
	if v.PageNum == 0 {
		v.PageNum = 1
	}
	if v.PageSize == 0 {
		v.PageSize = 30
	}
	if v.Column == "" {
		v.Column = "szse"
	}
	if v.TabName == "" {
		v.TabName = "fulltext"
	}
	if v.Stock == "" {
		v.Stock = "000001,gssz0000001"
	}
	if v.Category == "" {
		v.Category = "category_ndbg_szsh"
	}
	if v.IsHLtitle == "" {
		v.IsHLtitle = "true"
	}
	if v.SeDate == "" {
		now := time.Now()
		v.SeDate = fmt.Sprintf("%s~%s", now.AddDate(-1, 0, 0).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))
	}
	return fmt.Sprintf("pageNum=%d&pageSize=%d&column=%s&tabName=%s&plate=%s&stock=%s&searchkey=%s&secid=%s&category=%s&trade=%s&seDate=%s&sortName=%s&sortType=%s&isHLtitle=%s", v.PageNum, v.PageSize,
		url.QueryEscape(v.Column), url.QueryEscape(v.TabName), url.QueryEscape(v.Plate), url.QueryEscape(v.Stock), url.QueryEscape(v.Searchkey), url.QueryEscape(v.Secid), url.QueryEscape(v.Category),
		url.QueryEscape(v.Trade), v.SeDate, url.QueryEscape(v.SortName), url.QueryEscape(v.SortType), url.QueryEscape(v.IsHLtitle))
}

type Source struct {
}

func (s *Source) RequestStockList() (*StockListResponse, error) {
	resp, err := http.Get("http://www.cninfo.com.cn/new/data/szse_stock.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	p := &StockListResponse{}
	if err := json.NewDecoder(resp.Body).Decode(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Source) GetStockList() ([]*Stock, error) {
	p, err := s.RequestStockList()
	if err != nil {
		return nil, err
	}
	return p.StockList, nil
}

func (s *Source) RequestHisAnnouncementQuery(q *HisAnnouncementQueryRequest) (*HisAnnouncementQueryResponse, error) {
	client := &http.Client{}
	form := q.FormURLEncoded()
	req, err := http.NewRequest("POST", "http://www.cninfo.com.cn/new/hisAnnouncement/query", bytes.NewBufferString(form))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh-Hans;q=0.9")
	req.Header.Set("Host", "www.cninfo.com.cn")
	req.Header.Set("Origin", "http://www.cninfo.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.4 Safari/605.1.15")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", "http://www.cninfo.com.cn/new/commonUrl/pageOfSearch?url=disclosure/list/search&lastPage=index")
	req.Header.Set("Content-Length", strconv.Itoa(len(form)))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	p := &HisAnnouncementQueryResponse{}
	if err := json.NewDecoder(resp.Body).Decode(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Source) GetAnnualReportAnnoucements(stock *Stock, start, end time.Time) ([]*Announcement, error) {
	announcements := []*Announcement{}
	if start.After(end) {
		return nil, errors.New("start time must be before end time")
	}
	if end.Sub(start).Hours() > 24*365*30 {
		return nil, errors.New("time range must be less than 30 years")
	}

	from := start
	to := from.AddDate(3, 0, 0)
	if to.After(end) {
		to = end
	}
	for {
		p, err := s.RequestHisAnnouncementQuery(&HisAnnouncementQueryRequest{
			Stock:    strings.Join([]string{stock.Code, stock.OrgID}, ","),
			Category: "category_ndbg_szsh",
			SeDate:   fmt.Sprintf("%s~%s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		})
		if err != nil {
			return nil, err
		}
		c := len(p.Announcements)
		if c > 0 {
			for i := c - 1; i >= 0; i-- {
				announcements = append(announcements, p.Announcements[i])
			}
		}
		if to.Equal(end) || to.After(end) {
			break
		}
		from = to.AddDate(0, 0, 1)
		to = from.AddDate(3, 0, 0)
		if to.After(end) {
			to = end
		}
	}
	return announcements, nil
}

func (s *Source) RequestHisDividend(stockCode string) (*HisDividendResponse, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://www.cninfo.com.cn/data20/companyOverview/getCompanyHisDividend?scode="+stockCode, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	p := &HisDividendResponse{}
	if err := json.NewDecoder(resp.Body).Decode(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Source) GetDividendRecords(stock *Stock) ([]*DividendRecord, error) {
	p, err := s.RequestHisDividend(stock.Code)
	if err != nil {
		return nil, err
	}
	code := p.GetCodeString()
	if code != "200" {
		return nil, fmt.Errorf("code: %s, msg: %s", code, p.Msg)
	}
	return p.Data.Records, nil
}
