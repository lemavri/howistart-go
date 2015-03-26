package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {

	mw := multiWeatherProvider{
		openWeatherMap{},
		weatherUnderground{apiKey: "40ea560e7394ad7e"},
		forecast{apiKey: "f9b1c747e903aeba1637c39b35670424", requireCoords: true},
	}

	http.HandleFunc("/weather/", func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		city := strings.SplitN(r.URL.Path, "/", 3)[2]

		temp, err := mw.temperature(city)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"city": city,
			"temp": temp,
			"took": time.Since(begin).String(),
		})
	})

	http.ListenAndServe(":8080", nil)
}

type multiWeatherProvider []weatherProvider

type weatherProvider interface {
	temperature(city string) (float64, error) // in celsius
}

type openWeatherMap struct{}

type weatherUnderground struct {
	apiKey string
}

type forecast struct {
	apiKey        string
	requireCoords bool
}

func (w multiWeatherProvider) temperature(city string) (float64, error) {

	timedout := 0
	temps := make(chan float64, len(w))
	errs := make(chan error, len(w))

	for _, provider := range w {
		go func(p weatherProvider) {
			c, err := p.temperature(city)
			if err != nil {
				errs <- err
				return
			}
			temps <- c
		}(provider)
	}

	sum := 0.0

	for i := 0; i < len(w); i++ {
		select {
		case temp := <-temps:
			sum += temp
		case err := <-errs:
			return 0, err
		case <-time.After(2 * time.Second):
			timedout++
			return 0, nil
		}
	}

	return sum / (float64(len(w)) - float64(timedout)), nil
}

func (w openWeatherMap) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.openweathermap.org/data/2.5/weather?q=" + city)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Main struct {
			Kelvin float64 `json:"temp"`
		} `json:"main"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	celsius := d.Main.Kelvin - 273.15
	log.Printf("openWeatherMap: %s: %.2f", city, celsius)

	return celsius, nil
}

func (w weatherUnderground) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.wunderground.com/api/" + w.apiKey + "/conditions/q/" + city + ".json")
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Observation struct {
			Celsius float64 `json:"temp_c"`
		} `json:"current_observation"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	celsius := d.Observation.Celsius
	log.Printf("weatherUnderground: %s: %.2f", city, celsius)

	return celsius, nil
}

func (w forecast) temperature(city string) (float64, error) {
	lat, lng, err := getCoords(city)
	if err != nil {
		return 0, err
	}
	resp, err := http.Get("https://api.forecast.io/forecast/" + w.apiKey + "/" + strconv.FormatFloat(lat, 'f', 7, 64) + "," + strconv.FormatFloat(lng, 'f', 7, 64))

	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Currently struct {
			Farenheit float64 `json:"temperature"`
		} `json:"currently"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	celsius := (d.Currently.Farenheit - float64(32)) / 1.8
	log.Printf("forecast: %s: %.2f", city, celsius)

	return celsius, nil
}

func getCoords(city string) (float64, float64, error) {
	resp, err := http.Get("http://maps.googleapis.com/maps/api/geocode/json?address=" + city)
	if err != nil {
		return 0, 0, err
	}

	defer resp.Body.Close()

	var coords struct {
		Results []struct {
			Geometry struct {
				Location struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&coords); err != nil {
		return 0, 0, err
	}

	lat := coords.Results[0].Geometry.Location.Lat
	lng := coords.Results[0].Geometry.Location.Lng
	log.Printf("Coords of %s located: %f, %f", city, lat, lng)

	return lat, lng, nil
}
