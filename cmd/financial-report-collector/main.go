package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chamzzzzzz/financial/source/cninfo"
	"github.com/urfave/cli/v2"
)

type AnnualReport struct {
	Year        int
	Title       string
	URL         string
	PublishTime int64
}

type Stock struct {
	Code                  string
	Name                  string
	Pinyin                string
	OrgID                 string
	AnnualReportCheckTime int64
	AnnualReports         []*AnnualReport
}

type Database struct {
	Stocks  []*Stock
	indexes map[string]*Stock `json:"-"`
}

type Option struct {
	SinceYear2000              bool
	LatestThreeYears           bool
	SpecifiedYears             []int
	AnnualReportCheckInterval  int64
	AnnualReportUpdateInterval int64
	AnnualReportDownload       bool
	AnnualReportDownloadDir    string
	File                       string
}

type App struct {
	option   Option
	database Database
	start    time.Time
	end      time.Time
	years    []int
}

func (app *App) Run() error {
	c := &cli.App{
		Name:  "financial-report-collector",
		Usage: "financial-report-collector",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "since-year-2000",
				Usage:       "since year 2000",
				Destination: &app.option.SinceYear2000,
				Value:       false,
			},
			&cli.BoolFlag{
				Name:        "latest-three-years",
				Usage:       "latest three years",
				Destination: &app.option.LatestThreeYears,
				Value:       true,
			},
			&cli.MultiIntFlag{
				Target: &cli.IntSliceFlag{
					Name:  "specified-years",
					Usage: "specified years",
				},
				Value:       []int{},
				Destination: &app.option.SpecifiedYears,
			},
			&cli.Int64Flag{
				Name:        "annual-report-check-interval",
				Usage:       "annual report check interval",
				Destination: &app.option.AnnualReportCheckInterval,
				Value:       24 * 3600,
			},
			&cli.Int64Flag{
				Name:        "annual-report-update-interval",
				Usage:       "annual report update interval",
				Destination: &app.option.AnnualReportUpdateInterval,
				Value:       100,
			},
			&cli.BoolFlag{
				Name:        "annual-report-download",
				Usage:       "annual report download",
				Destination: &app.option.AnnualReportDownload,
				Value:       false,
			},
			&cli.StringFlag{
				Name:        "annual-report-download-dir",
				Usage:       "annual report download dir",
				Destination: &app.option.AnnualReportDownloadDir,
				Value:       "annualreport",
			},
			&cli.StringFlag{
				Name:        "file",
				Usage:       "database file",
				Destination: &app.option.File,
				Value:       "annualreport.db",
			},
		},
		Action: app.action,
	}
	return c.Run(os.Args)
}

func (app *App) loadDatabase() error {
	if _, err := os.Stat(app.option.File); err == nil {
		b, err := os.ReadFile(app.option.File)
		if err != nil {
			return err
		}
		err = json.Unmarshal(b, &app.database)
		if err != nil {
			return err
		}
	}
	app.database.indexes = make(map[string]*Stock)
	for _, stock := range app.database.Stocks {
		app.database.indexes[stock.Code] = stock
	}
	return nil
}

func (app *App) saveDatabase() error {
	b, err := json.MarshalIndent(&app.database, "", "  ")
	if err != nil {
		return err
	}
	os.WriteFile(app.option.File, b, 0644)
	return nil
}

func (app *App) updateStock() error {
	source := &cninfo.Source{}
	stocks, err := source.GetStockList()
	if err != nil {
		return err
	}
	for _, v := range stocks {
		if stock := app.database.indexes[v.Code]; stock == nil {
			stock = &Stock{
				Code:   v.Code,
				Name:   v.Zwjc,
				Pinyin: v.Pinyin,
				OrgID:  v.OrgID,
			}
			app.database.Stocks = append(app.database.Stocks, stock)
			app.database.indexes[stock.Code] = stock
			fmt.Printf("add stock %+v\n", stock)
		}
	}
	sort.Slice(app.database.Stocks, func(i, j int) bool { return app.database.Stocks[i].Code < app.database.Stocks[j].Code })
	return nil
}

func (app *App) updateAnnualReport() error {
	now := time.Now()
	var stocks []*Stock
	for _, stock := range app.database.Stocks {
		if now.Unix()-stock.AnnualReportCheckTime < app.option.AnnualReportCheckInterval {
			fmt.Printf("skip stock %s annual report\n", stock.Code)
			continue
		}
		stocks = append(stocks, stock)
	}

	if len(app.option.SpecifiedYears) > 0 {
		min, max := app.option.SpecifiedYears[0], app.option.SpecifiedYears[0]
		for _, year := range app.option.SpecifiedYears {
			if year < min {
				min = year
			}
			if year > max {
				max = year
			}
		}
		app.start = time.Date(min, 1, 1, 0, 0, 0, 0, time.Local)
		app.end = time.Date(max, 12, 31, 0, 0, 0, 0, time.Local)
		if app.start.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)) {
			app.start = time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
		}
		if app.end.After(now) {
			app.end = now
		}
	} else {
		if app.option.SinceYear2000 {
			app.start = time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
			app.end = now
		} else if app.option.LatestThreeYears {
			app.start = now.AddDate(-3, 0, 0)
			app.end = now
		}
		t := app.start
		for {
			if t.After(app.end) {
				break
			}
			app.years = append(app.years, t.Year())
			t = t.AddDate(1, 0, 0)
		}
	}

	i := 0
	for _, stock := range stocks {
		reports, err := app.getAnnualReports(stock)
		if err != nil {
			return err
		}
		for _, report := range reports {
			dup := false
			for _, v := range stock.AnnualReports {
				if *v == *report {
					fmt.Printf("skip stock %s dup annual report %+v\n", stock.Code, report)
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			stock.AnnualReports = append(stock.AnnualReports, report)
			fmt.Printf("add stock %s annual report %+v\n", stock.Code, report)
		}
		stock.AnnualReportCheckTime = time.Now().Unix()

		i++
		fmt.Printf("update stock annual report %d/%d\n", i, len(stocks))
		if i%10 == 0 {
			fmt.Printf("sleep %d milliseconds\n", app.option.AnnualReportUpdateInterval*2)
			time.Sleep(time.Duration(app.option.AnnualReportUpdateInterval * 2 * int64(time.Millisecond)))
			if err := app.saveDatabase(); err != nil {
				return err
			}
		} else {
			fmt.Printf("sleep %d milliseconds\n", app.option.AnnualReportUpdateInterval)
			time.Sleep(time.Duration(app.option.AnnualReportUpdateInterval * int64(time.Millisecond)))
		}
	}
	if err := app.saveDatabase(); err != nil {
		return err
	}
	return nil
}

func (app *App) getAnnualReports(stock *Stock) ([]*AnnualReport, error) {
	source := &cninfo.Source{}
	announcements, err := source.GetAnnualReportAnnoucements(&cninfo.Stock{Code: stock.Code, OrgID: stock.OrgID}, app.start, app.end)
	if err != nil {
		for i := 0; i < 3; i++ {
			fmt.Printf("get stock %s annual report error: %s\n", stock.Code, err)
			fmt.Printf("retry after 5 seconds\n")
			time.Sleep(time.Second * 5)
			announcements, err = source.GetAnnualReportAnnoucements(&cninfo.Stock{Code: stock.Code, OrgID: stock.OrgID}, app.start, app.end)
			if err == nil {
				break
			}
			if i == 2 {
				return nil, err
			}
		}
	}
	var reports []*AnnualReport
	for _, announcement := range announcements {
		if strings.ToUpper(announcement.AdjunctType) != "PDF" {
			continue
		}

		year := time.Unix(announcement.AnnouncementTime/1000, 0).Year() - 1
		matched := false
		for _, y := range app.years {
			if y == year {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		report := &AnnualReport{
			Year:        year,
			Title:       announcement.AnnouncementTitle,
			URL:         "http://static.cninfo.com.cn/" + announcement.AdjunctURL,
			PublishTime: announcement.AnnouncementTime,
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (app *App) downloadAnnualReport() error {
	if !app.option.AnnualReportDownload {
		return nil
	}

	for _, stock := range app.database.Stocks {
		if len(stock.AnnualReports) == 0 {
			continue
		}

		for _, report := range stock.AnnualReports {
			file := filepath.Join(app.option.AnnualReportDownloadDir, stock.Code, report.Title+".pdf")
			_, err := os.Stat(file)
			if err == nil {
				fmt.Printf("report %s already downloaded, skip.\n", file)
				continue
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}

			err = os.MkdirAll(filepath.Dir(file), 0777)
			if err != nil && errors.Is(err, fs.ErrExist) {
				return err
			}

			err = app.downloadURL(report.URL, file)
			if err != nil {
				return err
			}
			fmt.Printf("download annual report %s successed\n", file)
		}
	}
	return nil
}

func (app *App) downloadURL(URL, file string) error {
	resp, err := http.Get(URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code:%d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(b) != int(resp.ContentLength) {
		return fmt.Errorf("content length not match %d, %d", resp.ContentLength, len(b))
	}
	return os.WriteFile(file, b, 0666)
}

func (app *App) action(c *cli.Context) error {
	err := app.loadDatabase()
	if err != nil {
		return err
	}

	err = app.updateStock()
	if err != nil {
		return err
	}

	err = app.saveDatabase()
	if err != nil {
		return err
	}

	err = app.updateAnnualReport()
	if err != nil {
		return err
	}

	err = app.downloadAnnualReport()
	if err != nil {
		return err
	}
	return nil
}

func main() {
	app := &App{}
	err := app.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
