package exchange

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

// ErrInvalidCode is returned when the currency code is invalid
var ErrInvalidCode = errors.New("Invalid currency code")

// ErrInvalidDate is returned when the date is too old
var ErrInvalidDate = errors.New("Oldest possible date is 1999-01-04")

// ErrInvalidDateFormat is returned when the date isn't formatted as YYYY-MM-DD
var ErrInvalidDateFormat = errors.New("Date format must be YYYY-MM-DD")

// ErrTimeframeExceeded is returned when the time between start_date and end_date is bigger than 365 days
var ErrTimeframeExceeded = errors.New("Maximum allowed timeframe is 365 days")

// ErrInvalidTimeFrame is returned when the to date is older than to date. For example flipped the arguments
var ErrInvalidTimeFrame = errors.New("From date must be older than To date")

// ErrInvalidAPIResponse is returned when the API return success: false
var ErrInvalidAPIResponse = errors.New("Unknown API error")

const (
	baseURL             string = "https://api.exchangerate.host"
	symbolsURL          string = baseURL + "/symbols"
	cryptocurrenciesURL string = baseURL + "/cryptocurrencies"
	latestURL           string = baseURL + "/latest"
	convertURL          string = baseURL + "/convert"
	historicalURL       string = baseURL + "/"
	timeseriesURL       string = baseURL + "/timeseries"
	fluctuationURL      string = baseURL + "/fluctuation"
)

// Exchange is returned by New() and allows access to the methods
type Exchange struct {
	Base          string
	CacheEnabled  bool
	isInitialized bool // is set to true if used via New
}

type query struct {
	From      string
	To        string
	Base      string
	Amount    int
	Symbols   []string
	Date      string
	TimeFrame [2]string
}

var client http.Client = http.Client{
	Timeout: 10 * time.Second,
}
var cache *gocache.Cache

// New creates a new instance of Exchange
func New(base string) *Exchange {
	x := &Exchange{
		Base:          base,
		CacheEnabled:  true,
		isInitialized: true,
	}
	cache = gocache.New(cacheDuration(), 5*time.Minute)
	return x
}

// SetBase sets a new base currency for the exchange rates
func (exchange *Exchange) SetBase(base string) error {
	if err := ValidateCode(base); err != nil {
		return err
	}
	exchange.Base = base
	return nil
}

// SetCache enables and disable caching (caching last till midnight when the exchange rates are updated)
func (exchange *Exchange) SetCache(cache bool) {
	exchange.CacheEnabled = cache
}

func cacheDuration() time.Duration {
	now := time.Now().UTC()
	t := now.Add(time.Hour * 24)
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 5, 0, 0, time.UTC)
	timeTillMidnight := midnight.Sub(now)
	return timeTillMidnight
}

// ValidateCode validates a single symbol code
func ValidateCode(code string) error {
	if len(code) != 3 {
		return ErrInvalidCode
	}
	return nil
}

// ValidateSymbols validates all symbols' codes in an array
func ValidateSymbols(symbols []string) error {
	for code := range symbols {
		if err := ValidateCode(symbols[code]); err != nil {
			return err
		}
	}

	return nil
}

// ValidateDate validates date string according to YYYY-MM-DD format and if it's
func ValidateDate(date string) error {
	matched, err := regexp.Match("[0-9]{4,4}-((0[1-9])|(1[0-2]))-([0-3]{1}[0-9]{1})", []byte(date))
	if err != nil {
		return err
	}
	if !matched {
		return ErrInvalidDateFormat
	}
	oldestDate, _ := time.Parse("2006-01-02", "1999-01-03")
	selectedDate, _ := time.Parse("2006-01-02", date)
	if selectedDate.Before(oldestDate) {
		return ErrInvalidDate
	}
	return nil
}

// ValidateTimeFrame checks if the from and to date are not more than 365 days apart and they're not mixed
func ValidateTimeFrame(TimeFrame [2]string) error {
	from, _ := time.Parse("2006-01-02", TimeFrame[0])
	to, _ := time.Parse("2006-01-02", TimeFrame[1])
	if to.Before(from) {
		return ErrInvalidTimeFrame
	}

	if to.Sub(from).Hours() > 8759.992992006 {
		return ErrTimeframeExceeded
	}

	return nil
}

func (exchange *Exchange) get(url string, q query) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	processQuery(req, q)

	cacheKey := req.URL.String()

	if exchange.CacheEnabled {
		if response, ok := cache.Get(cacheKey); ok == true {
			return response.(map[string]interface{}), nil
		}
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	var result map[string]interface{}

	err = json.NewDecoder(resp.Body).Decode(&result)

	if err != nil {
		return nil, err
	}

	success := result["success"]

	if !success.(bool) {
		return nil, ErrInvalidAPIResponse
	}

	if exchange.CacheEnabled {
		cache.SetDefault(cacheKey, result)
	}

	return result, nil
}

func addToQuery(req *http.Request, key string, value string) {
	q := req.URL.Query()          // Get a copy of the query values.
	q.Add(key, value)             // Add a new value to the set.
	req.URL.RawQuery = q.Encode() // Encode and assign back to the original query.
}

func processQuery(req *http.Request, q query) error {
	if q.Base != "" {
		if err := ValidateCode(q.Base); err != nil {
			return err
		}
		addToQuery(req, "base", q.Base)
	}

	if q.From != "" {
		if err := ValidateCode(q.From); err != nil {
			return err
		}
		addToQuery(req, "from", q.From)
	}

	if q.To != "" {
		if err := ValidateCode(q.To); err != nil {
			return err
		}
		addToQuery(req, "to", q.To)
	}

	if q.Amount > 1 {
		addToQuery(req, "amount", strconv.Itoa(q.Amount))
	}

	if len(q.Symbols) != 0 {
		addToQuery(req, "symbols", strings.Join(q.Symbols, ","))
	}

	if q.Date != "" {
		if err := ValidateDate(q.Date); err != nil {
			return err
		}
		addToQuery(req, "date", q.Date)
	}

	if q.TimeFrame != [2]string{} {
		for i := 0; i < 1; i++ {
			if err := ValidateDate(q.TimeFrame[i]); err != nil {
				return err
			}
		}
		if err := ValidateTimeFrame(q.TimeFrame); err != nil {
			return err
		}
		addToQuery(req, "start_date", string(q.TimeFrame[0]))
		addToQuery(req, "end_date", string(q.TimeFrame[1]))
	}

	return nil
}

func (exchange *Exchange) apiSymbols() (map[string]map[string]string, error) {
	resp, err := exchange.get(symbolsURL, query{})
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for code, data := range resp["symbols"].(map[string]interface{}) {
		values := make(map[string]string)
		for name, value := range data.(map[string]interface{}) {
			values[name] = value.(string)
		}
		result[code] = values
	}
	return result, nil
}

func (exchange *Exchange) apiCryptocurrencies() (map[string]map[string]string, error) {
	resp, err := exchange.get(cryptocurrenciesURL, query{})
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for code, data := range resp["cryptocurrencies"].(map[string]interface{}) {
		values := make(map[string]string)
		for name, value := range data.(map[string]interface{}) {
			values[name] = value.(string)
		}
		result[code] = values
	}
	return result, nil
}

func (exchange *Exchange) apiLatest(q query) (map[string]*big.Float, error) {
	resp, err := exchange.get(latestURL, q)
	if err != nil {
		return nil, err
	}
	result := resp["rates"].(map[string]interface{})
	rates := make(map[string]*big.Float, len(result))
	for key := range result {
		rates[key] = big.NewFloat(result[key].(float64))
	}
	return rates, nil
}

func (exchange *Exchange) apiConvert(q query) (*big.Float, error) {
	resp, err := exchange.get(convertURL, q)
	if err != nil {
		return nil, err
	}
	result := resp["result"].(float64)
	return big.NewFloat(result), nil
}

func (exchange *Exchange) apiHistorical(q query) (map[string]*big.Float, error) {
	if err := ValidateDate(q.Date); err != nil {
		return nil, err
	}
	url := historicalURL + q.Date
	q.Date = ""
	resp, err := exchange.get(url, q)
	if err != nil {
		return nil, err
	}
	result := resp["rates"].(map[string]interface{})
	rates := make(map[string]*big.Float, len(result))
	for key := range result {
		rates[key] = big.NewFloat(result[key].(float64))
	}
	return rates, nil
}

func (exchange *Exchange) apiTimeseriesAndFuctuation(url string, q query) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.get(url, q)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]*big.Float)
	for date, rates := range resp["rates"].(map[string]interface{}) {
		ratemap := make(map[string]*big.Float)
		for symbol, rate := range rates.(map[string]interface{}) {
			frate := big.NewFloat(rate.(float64))
			ratemap[symbol] = frate
			result[date] = ratemap
		}
	}
	return result, nil
}

// ForexCodes returns and array of supported forex/fiat currency codes
func (exchange *Exchange) ForexCodes() ([]string, error) {
	var codes []string

	result, err := exchange.apiSymbols()
	if err != nil {
		return nil, err
	}

	for k := range result {
		codes = append(codes, k)
	}

	sort.Strings(codes)
	return codes, nil
}

// ForexData returns a map of supported forex/fiat currencies data (code & description)
func (exchange *Exchange) ForexData() (map[string]map[string]string, error) {
	return exchange.apiSymbols()
}

// CryptoCodes returns and array of supported cryptocurrency codes
func (exchange *Exchange) CryptoCodes() ([]string, error) {
	var codes []string

	result, err := exchange.apiCryptocurrencies()
	if err != nil {
		return nil, err
	}

	for k := range result {
		codes = append(codes, k)
	}

	sort.Strings(codes)
	return codes, nil
}

// CryptoData returns a map of supported cryptocurrencies data (name and symbol)
func (exchange *Exchange) CryptoData() (map[string]map[string]string, error) {
	return exchange.apiCryptocurrencies()
}

// LatestRatesAll returns the latest exchange rates for all supportedcurrencies
func (exchange *Exchange) LatestRatesAll() (map[string]*big.Float, error) {
	return exchange.apiLatest(query{Base: exchange.Base})
}

// LatestRatesMultiple returns the latest exchange rates for multiple currencies
func (exchange *Exchange) LatestRatesMultiple(symbols []string) (map[string]*big.Float, error) {
	return exchange.apiLatest(query{Base: exchange.Base, Symbols: symbols})

}

// LatestRatesSingle returns the latest exchange rates for a single currencies
func (exchange *Exchange) LatestRatesSingle(symbol string) (*big.Float, error) {
	resp, err := exchange.apiLatest(query{Base: exchange.Base, Symbols: []string{symbol}})
	if err != nil {
		return &big.Float{}, err
	}
	return resp[symbol], nil
}

// ConvertTo converts the amount from the exchange.Base currency to the target currency
func (exchange *Exchange) ConvertTo(target string, amount int) (*big.Float, error) {
	return exchange.apiConvert(query{From: exchange.Base, To: target, Amount: amount})
}

// ConvertAt converts the amount from the exchange.Base currency to the target currency
// at a selected historical date
func (exchange *Exchange) ConvertAt(date string, target string, amount int) (*big.Float, error) {
	return exchange.apiConvert(query{From: exchange.Base, To: target, Amount: amount, Date: date})
}

// HistoricalRatesAll returns the historical exchange rates for all supported currencies
func (exchange *Exchange) HistoricalRatesAll(date string) (map[string]*big.Float, error) {
	return exchange.apiHistorical(query{Base: exchange.Base, Date: date})
}

// HistoricalRatesMultiple returns the historical exchange rates for multiple currencies
func (exchange *Exchange) HistoricalRatesMultiple(date string, symbols []string) (map[string]*big.Float, error) {
	return exchange.apiHistorical(query{Base: exchange.Base, Symbols: symbols, Date: date})

}

// HistoricalRatesSingle returns the historical exchange rates for a single currency
func (exchange *Exchange) HistoricalRatesSingle(date string, symbol string) (*big.Float, error) {
	resp, err := exchange.apiHistorical(query{Base: exchange.Base, Symbols: []string{symbol}, Date: date})
	if err != nil {
		return &big.Float{}, err
	}
	return resp[symbol], nil
}

// TimeseriesAll returns the timeseries for all supported symbols
func (exchange *Exchange) TimeseriesAll(start string, end string) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(timeseriesURL, query{TimeFrame: [2]string{start, end}})
	return resp, err
}

// TimeseriesMultiple returns the timeseries for multiple symbols
func (exchange *Exchange) TimeseriesMultiple(start string, end string, symbols []string) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(timeseriesURL, query{TimeFrame: [2]string{start, end}, Symbols: symbols})
	return resp, err
}

// TimeseriesSingle returns the timeseries for a single symbol<
func (exchange *Exchange) TimeseriesSingle(start string, end string, symbol string) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(timeseriesURL, query{TimeFrame: [2]string{start, end}, Symbols: []string{symbol}})
	return resp, err
}

// FluctuationAll returns the fluctuation for all supported symbols
func (exchange *Exchange) FluctuationAll(start string, end string) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(fluctuationURL, query{TimeFrame: [2]string{start, end}})
	return resp, err
}

// FluctuationMultiple returns the fluctuation for multiple symbols
func (exchange *Exchange) FluctuationMultiple(start string, end string, symbols []string) (map[string]map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(fluctuationURL, query{TimeFrame: [2]string{start, end}, Symbols: symbols})
	return resp, err
}

// FluctuationSingle returns the fluctuation for a single symbol
func (exchange *Exchange) FluctuationSingle(start string, end string, symbol string) (map[string]*big.Float, error) {
	resp, err := exchange.apiTimeseriesAndFuctuation(fluctuationURL, query{TimeFrame: [2]string{start, end}, Symbols: []string{symbol}})
	return resp[symbol], err
}
