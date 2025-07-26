package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	tele "gopkg.in/telebot.v3"
)
var (
	db          *sql.DB
	weatherAPI  string
)
type WeatherData struct {
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
		Pressure  int     `json:"pressure"`
	} `json:"main"`
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
	Clouds struct {
		All int `json:"all"`
	} `json:"clouds"`
}
func init() {
	err := godotenv.Load("tokens.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	weatherAPI = os.Getenv("token_weather")
	db, err = sql.Open("sqlite3", "cities.db")
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			city_name TEXT
		)
	`)
	if err != nil {
		log.Fatal(err)
	}
}
func getWeather(city string) (*WeatherData, error) {
	url := fmt.Sprintf(
		"http://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&lang=ru&units=metric",
		city,
		weatherAPI,
	)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	var data WeatherData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}
func saveCity(userID int64, city string) error {
	_, err := db.Exec("INSERT INTO cities (user_id, city_name) VALUES (?, ?)", userID, city)
	return err
}
func getLastCities(userID int64) ([]string, error) {
	rows, err := db.Query(
		"SELECT city_name FROM cities WHERE user_id = ? ORDER BY id DESC LIMIT 5",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cities []string
	for rows.Next() {
		var city string
		if err := rows.Scan(&city); err != nil {
			return nil, err
		}
		cities = append(cities, city)
	}
	return cities, nil
}

func uniqueCities(cities []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, city := range cities {
		if !seen[city] {
			seen[city] = true
			result = append(result, city)
		}
	}
	return result
}
func main() {
	pref := tele.Settings{
		Token:  os.Getenv("token_api"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}
	b.Handle("/start", func(c tele.Context) error {
		return c.Send("Привет! Введи название города, чтобы получить прогноз погоды.")
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		city := c.Text()
		weather, err := getWeather(city)
		if err != nil {
			return c.Send("Не удалось получить данные о погоде. Проверьте название города.")
		}
		response := fmt.Sprintf(
			"Погода в городе %s:\nТемпература: %.1f°C\nСостояние: %s\n",
			city,
			weather.Main.Temp,
			weather.Weather[0].Description,
		)
		if err := saveCity(c.Sender().ID, city); err != nil {
			log.Println("Failed to save city:", err)
		}
		menu := &tele.ReplyMarkup{}
		btnCities := menu.Data("Показать последние введенные города", "show_last_cities")
		btnDetailed := menu.Data("Узнать более подробный прогноз", "detailed_forecast", city)

		menu.Inline(
			menu.Row(btnCities),
			menu.Row(btnDetailed),
		)
		c.Send(response)
		return c.Send(
			"Нажмите на кнопку ниже, чтобы увидеть последние введенные города или получить более подробный прогноз:",
			menu,
		)
	})
	b.Handle(tele.OnCallback, func(c tele.Context) error {
		data := c.Callback().Data

		switch {
		case data == "show_last_cities":
			cities, err := getLastCities(c.Sender().ID)
			if err != nil {
				log.Println(err)
				return c.Send("Произошла ошибка при получении городов")
			}

			if len(cities) == 0 {
				return c.Send("У вас пока нет введенных городов.")
			}

			menu := &tele.ReplyMarkup{}
			for _, city := range uniqueCities(cities) {
				menu.Data(city, "weather", city)
			}

			return c.Send("Выберите город, прогноз для которого хотите узнать:", menu)

		case strings.HasPrefix(data, "weather"):
			city := strings.Split(data, "|")[1]
			weather, err := getWeather(city)
			if err != nil {
				return c.Send("Не удалось получить данные о погоде для этого города.")
			}
			response := fmt.Sprintf(
				"Погода в городе %s:\nТемпература: %.1f°C\nСостояние: %s\nВлажность: %d%%\n",
				city,
				weather.Main.Temp,
				weather.Weather[0].Description,
				weather.Main.Humidity,
			)

			return c.Send(response)
		case strings.HasPrefix(data, "detailed_forecast"):
			city := strings.Split(data, "|")[1]
			weather, err := getWeather(city)
			if err != nil {
				return c.Send("Не удалось получить данные о погоде для этого города.")
			}

			response := fmt.Sprintf(
				"Подробный прогноз погоды в городе %s:\n"+
					"Температура: %.1f°C\n"+
					"Ощущается как: %.1f°C\n"+
					"Состояние: %s\n"+
					"Влажность: %d%%\n"+
					"Давление: %d гПа\n"+
					"Скорость ветра: %.1f м/с\n"+
					"Облачность: %d%%\n",
				city,
				weather.Main.Temp,
				weather.Main.FeelsLike,
				weather.Weather[0].Description,
				weather.Main.Humidity,
				weather.Main.Pressure,
				weather.Wind.Speed,
				weather.Clouds.All,
			)

			return c.Send(response)
		}

		return nil
	})

	b.Start()
}
