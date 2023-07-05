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
	codes   = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_CODES")
	addr    = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_ADDR")
	user    = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_USER")
	pass    = os.Getenv("FINANCIAL_DIVIDEND_MONITOR_SMTP_PASS")
	source  = "From: {{.From}}\r\nTo: {{.To}}\r\nSubject: {{.Subject}}\r\n\r\n{{.Body}}"
	tpl     *template.Template
	stocks  []*cninfo.Stock
	monitor bool
)

func main() {
	flag.BoolVar(&monitor, "monitor", false, "monitor")
	flag.StringVar(&codes, "codes", codes, "stock code list to monitor or update, separated by comma. eg: 000001,000002")
	flag.StringVar(&addr, "addr", addr, "notification smtp addr")
	flag.StringVar(&user, "user", user, "notification smtp user")
	flag.StringVar(&pass, "pass", pass, "notification smtp pass")
	flag.Parse()

	s, err := (&cninfo.Source{}).GetStockList()
	if err != nil {
		log.Printf("get stock list fail. err='%s'", err)
		return
	}
	if !monitor {
		err := writeStocks(s)
		if err != nil {
			log.Printf("write stock list fail. err='%s'", err)
			return
		}
	}
	m := make(map[string]*cninfo.Stock)
	for _, stock := range s {
		m[stock.Code] = stock
	}

	c := strings.Split(codes, ",")
	if b, err := os.ReadFile("codes.txt"); err == nil {
		c = append(c, strings.Split(string(b), "\n")...)
	} else if !os.IsNotExist(err) {
		log.Printf("read codes.txt fail. err='%s'", err)
		return
	}

	d := make(map[string]struct{})
	for _, code := range c {
		if code == "" {
			continue
		}
		if _, ok := d[code]; ok {
			continue
		}
		stock := m[code]
		if stock == nil {
			log.Printf("stock not found. code=%s", code)
			return
		}
		stocks = append(stocks, stock)
		d[code] = struct{}{}
		if monitor {
			log.Printf("monitoring stock. code=%s", code)
		} else {
			log.Printf("updating stock. code=%s", code)
		}
	}

	if len(stocks) == 0 {
		if monitor {
			log.Printf("no stock to monitor")
		} else {
			log.Printf("no stock to update")
		}
		return
	}

	funcs := template.FuncMap{
		"bencoding": mime.BEncoding.Encode,
	}
	tpl = template.Must(template.New("mail").Funcs(funcs).Parse(source))

	for {
		check()
		if !monitor {
			break
		}
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
		if !monitor {
			err = writeStockDividendRecords(stock, records)
			if err != nil {
				log.Printf("check fail, write dividend records error. code=%s, err='%v'", stock.Code, err)
				continue
			}
		}
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

func writeStocks(stocks []*cninfo.Stock) error {
	if len(stocks) == 0 {
		log.Printf("write stock list skip. no stock")
		return nil
	}
	var buf bytes.Buffer
	for _, stock := range stocks {
		if _, err := buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n", stock.Code, stock.Zwjc, stock.Pinyin, stock.Category, stock.OrgID)); err != nil {
			log.Printf("write stock list fail. err='%s'", err)
			return err
		}
	}
	err := os.WriteFile("stocks.txt", buf.Bytes(), 0644)
	if err != nil {
		log.Printf("write stock list fail. err='%s'", err)
		return err
	}
	log.Printf("write stock list success.")
	return nil
}
