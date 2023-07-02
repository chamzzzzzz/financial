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
	"strings"
	"text/template"
	"time"

	"github.com/chamzzzzzz/financial/source/cninfo"
)

var (
	codes  = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_CODES")
	addr   = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_ADDR")
	user   = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_USER")
	pass   = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_PASS")
	source = "From: {{.From}}\r\nTo: {{.To}}\r\nSubject: {{.Subject}}\r\n\r\n{{.Body}}"
	tpl    *template.Template
	stocks []*cninfo.Stock
)

func main() {
	flag.StringVar(&codes, "codes", codes, "stock code list to monitor, separated by comma. eg: 000001,000002")
	flag.StringVar(&addr, "addr", addr, "notification smtp addr")
	flag.StringVar(&user, "user", user, "notification smtp user")
	flag.StringVar(&pass, "pass", pass, "notification smtp pass")
	flag.Parse()

	s, err := (&cninfo.Source{}).GetStockList()
	if err != nil {
		log.Printf("get stock list fail. err='%s'", err)
		return
	}
	m := make(map[string]*cninfo.Stock)
	for _, stock := range s {
		m[stock.Code] = stock
	}
	for _, code := range strings.Split(codes, ",") {
		if code == "" {
			continue
		}
		stock := m[code]
		if stock == nil {
			log.Printf("stock not found. code=%s", code)
			return
		}
		stocks = append(stocks, stock)
		log.Printf("monitoring stock. code=%s", code)
	}
	if len(stocks) == 0 {
		log.Printf("no stock to monitor")
		return
	}

	funcs := template.FuncMap{
		"bencoding": mime.BEncoding.Encode,
	}
	tpl = template.Must(template.New("mail").Funcs(funcs).Parse(source))

	for {
		check()
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, now.Location())
		log.Printf("next check at %s\n", next.Format("2006-01-02 15:04:05"))
		time.Sleep(next.Sub(now))
	}
}

func check() {
	log.Printf("check start at %s", time.Now().Format("2006-01-02 15:04:05"))
	t := time.Now()

	source := &cninfo.Source{}
	for _, stock := range stocks {
		records, err := source.GetDividendRecords(stock)
		if err != nil {
			log.Printf("check fail, get dividend records error. code=%s, err='%v'", stock.Code, err)
			continue
		}
		notification(stock, records)
	}
	log.Printf("check done at %s, cost %s", time.Now().Format("2006-01-02 15:04:05"), time.Since(t))
}

func notification(stock *cninfo.Stock, records []*cninfo.DividendRecord) {
	type Data struct {
		From    string
		To      string
		Subject string
		Body    string
		Stock   *cninfo.Stock
		Records []*cninfo.DividendRecord
	}

	if len(records) == 0 {
		log.Printf("send notification skip. no dividend record")
		return
	}
	latest := records[0]
	if latest.PayDate != time.Now().Format("2006-01-02") {
		return
	}

	log.Printf("sending notification...")
	if addr == "" {
		log.Printf("send notification skip. addr is empty")
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("send notification fail. err='%s'\n", err)
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
		From:    fmt.Sprintf("%s <%s>", mime.BEncoding.Encode("UTF-8", "Monitor"), user),
		To:      user,
		Subject: mime.BEncoding.Encode("UTF-8", fmt.Sprintf("「FIN」%s", subject)),
		Body:    body,
		Stock:   stock,
		Records: records,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("send notification fail. err='%s'\n", err)
		return
	}

	auth := smtp.PlainAuth("", user, pass, host)
	if err := smtp.SendMail(addr, auth, user, []string{user}, buf.Bytes()); err != nil {
		log.Printf("send notification fail. err='%s'\n", err)
	}
	log.Printf("send notification success.\n")
}
