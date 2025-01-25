package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
	"golang.org/x/text/width"
	"k8s.io/utils/strings/slices"
)

func env_load() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func ParseFlag() (*int, *int, *int, *int) {
	//変数でflagを定義します
	var (
		year  = flag.Int("year", 0, "-year=2024")
		month = flag.Int("month", 0, "-month=8")
		from  = flag.Int("from", 0, "-from=1")
		to    = flag.Int("to", 0, "-to=31")
	)
	//ここで解析されます
	flag.Parse()

	if *year == 0 || *month == 0 || *from == 0 || *to == 0 {
		log.Fatal("year, month, from, to are required")
	}

	return year, month, from, to
}

func main() {
	const LOGIN_URL = "https://ec.coopdeli.jp/auth/login.html"
	const INVOICE_URL = "https://ec.coopdeli.jp/mypage/delivery/daily_detail.html"

	year, month, from, to := ParseFlag()
	env_load()

	// Cookie jarを作成
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatal(err)
	}

	// HTTPクライアントを作成
	client := &http.Client{
		Jar: jar,
	}

	// ログインページをGETリクエストで取得
	resp, err := client.Get(LOGIN_URL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	// ログインページをGETリクエストで取得してCSRFトークンを取得
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	csrfToken, exists := doc.Find("input[name='$csrfToken']").Attr("value")
	if !exists {
		log.Fatal("CSRF token not found")
	}

	// ログインデータを作成
	loginData := url.Values{
		"j_username": {os.Getenv("USERNAME")},
		"j_password": {os.Getenv("PASSWORD")},
		"action":     {""},
		"PageID":     {"WCSBFE15"},
		"successuri": {""},
		"$csrfToken": {csrfToken},
	}

	// ログインリクエストを送信
	req, err := http.NewRequest("POST", LOGIN_URL, strings.NewReader(loginData.Encode()))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err = client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	// ログイン成功を確認
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to login, status code: %d", resp.StatusCode)
	}

	// CSVファイルを作成
	file, err := os.Create("coop_data.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// ヘッダーを書き込む（必要に応じて修正）
	writer.Write([]string{"Date", "Product", "Price"}) // 適切なヘッダーに変更してください

	output_price := 0

	for day := *from; day <= *to; day++ {
		date := time.Date(*year, time.Month(*month), day, 0, 0, 0, 0, time.Local)
		week := (day-1)/7 + 1
		dayOfWeek := int(date.Weekday()) + 1 // 0 = Sunday, 1 = Monday, ..., 6 = Saturday

		yyyymm := fmt.Sprintf("%d%02d", *year, *month)

		swn := fmt.Sprintf("%s%02d@%d@240", yyyymm, week, dayOfWeek)

		// データ取得リクエストを送信
		dataPayload := url.Values{
			"PageID": {"WEKPEA0740"},
			"dcd":    {"9100299751"},
			"swn":    {swn},
		}

		req, err = http.NewRequest("POST", INVOICE_URL, strings.NewReader(dataPayload.Encode()))
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		resp, err = client.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()

		// レスポンスを読み取る
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		// レスポンスHTMLをパース
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			log.Fatal(err)
		}

		// 舞菜の単価を抽出
		doc.Find("div.billingSheet").Each(func(i int, s *goquery.Selection) {
			s.Contents().Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				// 舞菜の行を見つけたら単価を取得
				if strings.Contains(text, "舞菜") {
					lines := strings.Split(text, " ")
					for _, line := range lines {
						if strings.Contains(line, "舞菜") {
							parts := strings.Fields(line)

							// [０７／０４ （０７／０４）舞菜 おかず －６２０ １ －６２０ ◇返金します]
							if slices.Contains(parts, "◇返金します") {
								product := parts[2]
								price := width.Fold.String(parts[5])

								// CSVに書き出すデータ
								data := []string{date.Format("2006-01-02"), product, price}
								// データを書き出し
								err = writer.Write(data)
								if err != nil {
									log.Fatal(err)
								}

								// 合計金額に追加
								int_price, err := strconv.Atoi(price)
								if err != nil {
									log.Fatal(err)
								}
								output_price += int_price
								// [メインメニュー ９０２ 舞菜 おかず ６２０ １ ６２０ ◇]
							} else if len(parts) > 2 {
								product := parts[2] + parts[3]
								price := width.Fold.String(parts[6])

								// CSVに書き出すデータ
								data := []string{date.Format("2006-01-02"), product, price}

								// データを書き出し
								err = writer.Write(data)
								if err != nil {
									log.Fatal(err)
								}

								// 合計金額に追加
								int_price, err := strconv.Atoi(price)
								if err != nil {
									log.Fatal(err)
								}
								output_price += int_price
							} else {
								fmt.Printf("date: %s 舞菜の単価が見つかりませんでした\n", date.Format("2006-01-02"))
								continue
							}
						}
					}
				}
			})
		})
	}

	fmt.Println("データ取得完了")

	fmt.Println("合計金額: ", output_price)
}
