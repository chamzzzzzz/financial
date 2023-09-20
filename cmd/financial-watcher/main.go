package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/chamzzzzzz/financial/source/cninfo"
)

var (
	addr = os.Getenv("FINANCIAL_WATCHER_SMTP_ADDR")
	user = os.Getenv("FINANCIAL_WATCHER_SMTP_USER")
	pass = os.Getenv("FINANCIAL_WATCHER_SMTP_PASS")
	tpl  = template.Must(template.New("").Parse("From: {{.From}}\r\nTo: {{.To}}\r\nSubject: {{.Subject}}\r\n\r\n{{.Body}}"))
)

func main() {
	var w1, w2, w3 bool
	flag.BoolVar(&w1, "stock", false, "stock code")
	flag.BoolVar(&w2, "report", false, "stock report")
	flag.BoolVar(&w3, "dividend", false, "stock dividend")
	flag.StringVar(&addr, "addr", addr, "notification smtp addr")
	flag.StringVar(&user, "user", user, "notification smtp user")
	flag.StringVar(&pass, "pass", pass, "notification smtp pass")
	flag.Parse()

	source := &cninfo.Source{}
	if w1 {
		stocks, err := source.GetStockList()
		if err != nil {
			log.Printf("get stock fail. err='%s'", err)
			return
		}
		err = writeStocks("stock.txt", stocks)
		if err != nil {
			log.Printf("write stock fail. err='%s'", err)
			return
		}
		log.Printf("write stock success. count=%d", len(stocks))
	}

	if !w2 && !w3 {
		return
	}

	stocks, err := readStocks("watch.txt")
	if err != nil {
		log.Printf("read watch stock fail. err='%s'", err)
		return
	}
	if len(stocks) == 0 {
		log.Printf("no watch stock.")
		return
	}

	if w2 {
		for _, stock := range stocks {
			diff := false
			start, end := time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local), time.Now()
			announcements, err := readStockReportAnnouncements(stock)
			if err != nil {
				log.Printf("read stock report announcements error. code=%s, err='%v'", stock.Code, err)
				continue
			}
			if len(announcements) > 0 {
				start = end.AddDate(-3, 0, 0)
				diff = true
			}
			_announcements, err := source.GetAnnualReportAnnoucements(stock, start, end)
			if err != nil {
				log.Printf("get stock report announcements error. code=%s, err='%v'", stock.Code, err)
				continue
			}
			var __announcements []*cninfo.Announcement
			for _, _announcement := range _announcements {
				has := false
				for _, announcement := range announcements {
					if announcement.AnnouncementID == _announcement.AnnouncementID {
						has = true
						break
					}
				}
				if !has {
					announcements = append(announcements, _announcement)
					__announcements = append(__announcements, _announcement)
				}
			}
			if len(__announcements) == 0 {
				continue
			}
			sortStockReportAnnouncements(announcements)
			if err = writeStockReportAnnouncements(stock, announcements); err != nil {
				log.Printf("write stock report announcements error. code=%s, err='%v'", stock.Code, err)
				continue
			}
			if diff {
				sortStockReportAnnouncements(__announcements)
				sendStockReportAnnouncementsNotification(stock, __announcements)
			}
		}
	}

	if w3 {
		for _, stock := range stocks {
			records, err := source.GetDividendRecords(stock)
			if err != nil {
				log.Printf("get stock dividend records error. code=%s, err='%v'", stock.Code, err)
				continue
			}
			err = writeStockDividendRecords(stock, records)
			if err != nil {
				log.Printf("write stock dividend records error. code=%s, err='%v'", stock.Code, err)
				continue
			}
			sendStockDividendRecordsNotification(stock, records)
		}
	}
}

func sendStockDividendRecordsNotification(stock *cninfo.Stock, records []*cninfo.DividendRecord) {
	type Data struct {
		From        string
		To          string
		Subject     string
		ContentType string
		Body        string
		Stock       *cninfo.Stock
		Records     []*cninfo.DividendRecord
	}

	if len(records) == 0 {
		log.Printf("send stock dividend records notification skip. no dividend record")
		return
	}
	latest := records[0]
	if latest.PayDate != time.Now().Format("2006-01-02") {
		return
	}

	log.Printf("sending stock dividend records notification...")
	if addr == "" {
		log.Printf("send stock dividend records notification skip. addr is empty")
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("send stock dividend records notification fail. err='%s'\n", err)
		return
	}

	subject := fmt.Sprintf("%s(%s) 分红信息", stock.Zwjc, stock.Code)
	body := "今日以及历史分红信息\r\n\r\n"
	for _, record := range records {
		period := record.Period
		if record.Period == "" {
			period = "特别分红"
		}
		body += fmt.Sprintf("%s %s %s\r\n", period, record.Plan, record.PayDate)
	}
	data := Data{
		From:        fmt.Sprintf("%s <%s>", mime.BEncoding.Encode("UTF-8", "Monitor"), user),
		To:          user,
		Subject:     mime.BEncoding.Encode("UTF-8", fmt.Sprintf("「FIN」%s", subject)),
		ContentType: "text/plain; charset=utf-8",
		Body:        body,
		Stock:       stock,
		Records:     records,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("send stock dividend records notification fail. err='%s'\n", err)
		return
	}

	for i := 0; i < 3; i++ {
		auth := smtp.PlainAuth("", user, pass, host)
		if err := smtp.SendMail(addr, auth, user, []string{user}, buf.Bytes()); err != nil {
			log.Printf("send stock dividend records notification fail, retry after 10s. err='%s'", err)
			time.Sleep(time.Second * 10)
			continue
		}
		log.Printf("send stock dividend records notification success.\n")
		break
	}
}

func sendStockReportAnnouncementsNotification(stock *cninfo.Stock, announcements []*cninfo.Announcement) {
	type Data struct {
		From          string
		To            string
		Subject       string
		ContentType   string
		Body          string
		Stock         *cninfo.Stock
		Announcements []*cninfo.Announcement
	}

	if len(announcements) == 0 {
		log.Printf("send stock report announcements notification skip. no report announcements")
		return
	}

	log.Printf("sending stock report announcements notification...")
	if addr == "" {
		log.Printf("send stock report announcements notification skip. addr is empty")
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("send stock report announcements notification fail. err='%s'\n", err)
		return
	}

	subject := fmt.Sprintf("%s(%s) 年度报告", stock.Zwjc, stock.Code)
	body := "今日年度报告发布\r\n\r\n"
	for _, announcement := range announcements {
		body += fmt.Sprintf("%s\r\n", announcement.AnnouncementTitle)
	}
	data := Data{
		From:          fmt.Sprintf("%s <%s>", mime.BEncoding.Encode("UTF-8", "Monitor"), user),
		To:            user,
		Subject:       mime.BEncoding.Encode("UTF-8", fmt.Sprintf("「FIN」%s", subject)),
		ContentType:   "text/plain; charset=utf-8",
		Body:          body,
		Stock:         stock,
		Announcements: announcements,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("send stock report announcements notification fail. err='%s'\n", err)
		return
	}

	for i := 0; i < 3; i++ {
		auth := smtp.PlainAuth("", user, pass, host)
		if err := smtp.SendMail(addr, auth, user, []string{user}, buf.Bytes()); err != nil {
			log.Printf("send stock report announcements notification fail, retry after 10s. err='%s'", err)
			time.Sleep(time.Second * 10)
			continue
		}
		log.Printf("send stock report announcements notification success.\n")
		break
	}
}

func writeStockDividendRecords(stock *cninfo.Stock, records []*cninfo.DividendRecord) error {
	if len(records) == 0 {
		log.Printf("write stock dividend records skip. no dividend record. code=%s", stock.Code)
		return nil
	}
	var buf bytes.Buffer
	for _, record := range records {
		period := record.Period
		if record.Period == "" {
			period = "特别分红"
		}
		payDate := record.PayDate
		if record.PayDate == "" {
			payDate = "--"
		}
		if _, err := buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n", period, record.Plan, record.RecordDate, record.ExDividendDate, payDate)); err != nil {
			log.Printf("write stock dividend records fail. code=%s, err='%s'", stock.Code, err)
			return err
		}
	}
	os.MkdirAll("dividend", 0755)
	err := os.WriteFile(fmt.Sprintf("dividend/%s.txt", stock.Code), buf.Bytes(), 0644)
	if err != nil {
		log.Printf("write stock dividend records fail. code=%s, err='%s'", stock.Code, err)
		return err
	}
	log.Printf("write stock dividend records success. code=%s", stock.Code)
	return nil
}

func writeStocks(name string, stocks []*cninfo.Stock) error {
	if len(stocks) == 0 {
		return fmt.Errorf("no stock")
	}
	var buf bytes.Buffer
	for _, stock := range stocks {
		if _, err := buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n", stock.Code, stock.Zwjc, stock.Pinyin, stock.Category, stock.OrgID)); err != nil {
			return err
		}
	}
	return os.WriteFile(name, buf.Bytes(), 0644)
}

func readStocks(name string) ([]*cninfo.Stock, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stocks []*cninfo.Stock
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 5 {
			return nil, fmt.Errorf("invalid line")
		}
		stocks = append(stocks, &cninfo.Stock{
			Code:     f[0],
			Zwjc:     f[1],
			Pinyin:   f[2],
			Category: f[3],
			OrgID:    f[4],
		})
	}
	return stocks, nil
}

func writeStockReportAnnouncements(stock *cninfo.Stock, announcements []*cninfo.Announcement) error {
	if len(announcements) == 0 {
		return fmt.Errorf("no announcement")
	}
	var buf bytes.Buffer
	for _, announcement := range announcements {
		if _, err := buf.WriteString(fmt.Sprintf("%s,%s,%s\n", announcement.AnnouncementID, announcement.AnnouncementTitle, announcement.AdjunctURL)); err != nil {
			return err
		}
	}
	os.MkdirAll("report", 0755)
	return os.WriteFile(fmt.Sprintf("report/%s.txt", stock.Code), buf.Bytes(), 0644)
}

func readStockReportAnnouncements(stock *cninfo.Stock) ([]*cninfo.Announcement, error) {
	b, err := os.ReadFile(fmt.Sprintf("report/%s.txt", stock.Code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var announcements []*cninfo.Announcement
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 3 {
			return nil, fmt.Errorf("invalid line")
		}
		announcements = append(announcements, &cninfo.Announcement{
			AnnouncementID:    f[0],
			AnnouncementTitle: f[1],
			AdjunctURL:        f[2],
		})
	}
	return announcements, nil
}

func sortStockReportAnnouncements(announcements []*cninfo.Announcement) {
	sort.Slice(announcements, func(i, j int) bool {
		if announcements[i].AnnouncementTime == announcements[j].AnnouncementTime {
			ii, _ := strconv.ParseUint(announcements[i].AnnouncementID, 10, 64)
			jj, _ := strconv.ParseUint(announcements[j].AnnouncementID, 10, 64)
			return ii > jj
		}
		return announcements[i].AnnouncementTime > announcements[j].AnnouncementTime
	})
}
