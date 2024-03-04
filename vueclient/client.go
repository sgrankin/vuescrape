package vueclient

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const authClientID = "4qte47jbstod8apnfic0bunmrq"
const userPool = "us-east-2_ghlOXVLi1"
const authRegion = "us-east-2"

var apiBase, _ = url.Parse("https://api.emporiaenergy.com")

// Client is an Emporia Vue API client.
// See [api docs] for details on the protocol.
// [api docs]: https://github.com/magico13/PyEmVue/blob/master/api_docs.md
type Client struct {
	hc *http.Client
}

func NewClient(tok *Atom[*Token], authFunc func() (string, string, error)) *Client {
	return &Client{&http.Client{
		Transport: &throttledTransport{
			Limiter: rate.NewLimiter(rate.Limit(10), 1), // 10/s
			Base: &cognitoAuthTransport{
				Base: http.DefaultTransport,
				Source: &CognitoTokenSource{
					Cognito: &Cognito{
						Region:   authRegion,
						ClientID: authClientID,
						UserPool: userPool,
					},
					Tok:      tok,
					AuthFunc: authFunc,
				},
			},
		}}}
}

// GetDevices fetches all the customer devices.
func (c *Client) GetDevices() ([]Device, error) {
	u := apiBase.JoinPath("/customers/devices")
	rep, err := c.hc.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rep.Body)
		return nil, fmt.Errorf("request failed: %s: %s", rep.Status, body)
	}
	var body struct {
		Devices []Device `json:"devices"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Devices, nil
}

// GetUsage fetches the current usage values for the given scale.
func (c *Client) GetUsage(devices []DeviceGID, instant time.Time, scale Scale, energyUnit EnergyUnit) (time.Time, []DeviceUsage, error) {
	v := url.Values{}
	v.Set("apiMethod", "getDeviceListUsages")
	v.Set("deviceGids", strings.Trim(fmt.Sprint(devices), "[]"))
	v.Set("instant", instant.UTC().Format(time.RFC3339))
	v.Set("scale", string(scale))
	v.Set("energyUnit", string(energyUnit))

	u := apiBase.JoinPath("/AppAPI")
	u.RawQuery = v.Encode()
	rep, err := c.hc.Get(u.String())
	if err != nil {
		return time.Time{}, nil, err
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rep.Body)
		return time.Time{}, nil, fmt.Errorf("request failed: %s: %s", rep.Status, body)
	}
	var body struct {
		DeviceListUsages struct {
			Instant time.Time     `json:"instant"`
			Scale   Scale         `json:"scale"`
			Devices []DeviceUsage `json:"devices"`
		} `json:"deviceListUsages"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&body); err != nil {
		return time.Time{}, nil, err
	}
	return body.DeviceListUsages.Instant, body.DeviceListUsages.Devices, nil
}

// GetHistory fetches the chart history for the channel.
//
// Multiple requests are performed as needed to paginate the results.
// Not all scale sizes are supported due to unknown page sizes.
// As data is aggregated serverside, a limited view is available.
func (c *Client) GetHistory(device DeviceGID, channel string, start, end time.Time, scale Scale, energyUnit EnergyUnit) (time.Time, []*float64, error) {
	var inst time.Time
	var samples []*float64
	for start != end {
		pageEnd := end
		if pageEnd.After(start.Add(scale.PageSize())) {
			pageEnd = start.Add(scale.PageSize())
		}
		pinst, psamples, err := c.GetHistoryPage(device, channel, start, pageEnd, scale, energyUnit)
		if err != nil {
			return time.Time{}, nil, err
		}
		if inst.IsZero() {
			inst = pinst
		}
		samples = append(samples, psamples...)
		start = pageEnd
	}
	return inst, samples, nil
}

// GetHistoryPage issues a single request to fetch a page of chart data for the channel.
func (c *Client) GetHistoryPage(device DeviceGID, channel string, start, end time.Time, scale Scale, energyUnit EnergyUnit) (time.Time, []*float64, error) {
	log.Printf("getChartUsage %v %s (%s -> %s) %s %s", device, channel, start, end, scale, energyUnit)
	v := url.Values{}
	v.Set("apiMethod", "getChartUsage")
	v.Set("deviceGid", fmt.Sprint(device))
	v.Set("channel", channel)
	v.Set("start", start.UTC().Format(time.RFC3339))
	v.Set("end", end.UTC().Format(time.RFC3339))
	v.Set("scale", string(scale))
	v.Set("energyUnit", string(energyUnit))

	u := apiBase.JoinPath("/AppAPI")
	u.RawQuery = v.Encode()
	rep, err := c.hc.Get(u.String())
	if err != nil {
		return time.Time{}, nil, err
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rep.Body)
		return time.Time{}, nil, fmt.Errorf("request failed: %s: %s", rep.Status, body)
	}
	var body struct {
		UsageList         []*float64 `json:"usageList"`
		FirstUsageInstant time.Time  `json:"firstUsageInstant"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&body); err != nil {
		return time.Time{}, nil, err
	}
	return body.FirstUsageInstant, body.UsageList, nil
}

type Device struct {
	DeviceGID DeviceGID `json:"deviceGid"`
	Model     string    `json:"model"`
	Channels  []Channel `json:"channels"`
	Devices   []Device  `json:"devices"`
}

type Channel struct {
	Name              string    `json:"name"`
	DeviceGID         DeviceGID `json:"deviceGid"`  // Same as the Parent device ID.  XXX remove?
	ChannelNum        string    `json:"channelNum"` // "1,2,3", "1", "Balance"
	ChannelMultiplier float64   `json:"channelMultiplier"`
}

type DeviceUsage struct {
	DeviceGID     DeviceGID `json:"deviceGid"`
	ChannelUsages []struct {
		Name          string        `json:"name"`
		Usage         float64       `json:"usage"`
		ChannelNum    string        `json:"channelNum"`
		NestedDevices []DeviceUsage `json:"nestedDevices"`
	} `json:"channelUsages"`
}

type DeviceGID int

type EnergyUnit string

const (
	EnergyKWh EnergyUnit = "KilowattHours"
	EnergyAh  EnergyUnit = "AmpHours"

	EnergyUSD          EnergyUnit = "Dollars"
	EnergyTrees        EnergyUnit = "Trees"
	EnergyGallonsOfGas EnergyUnit = "GallonsOfGas"
	EnergyMilesDriven  EnergyUnit = "MilesDriven"
	EnergyCarbon       EnergyUnit = "Carbon"
)

type Scale string

const (
	Scale1Second Scale = "1S"
	Scale1Minute Scale = "1MIN"
	Scale1Hour   Scale = "1H"
	Scale1Day    Scale = "1D"
	Scale1Week   Scale = "1W"
	Scale1Month  Scale = "1MON"
	Sscale1Year  Scale = "1Y"
)

var scaleDurations = map[Scale]time.Duration{
	Scale1Second: time.Second,
	Scale1Minute: time.Minute,
	Scale1Hour:   time.Hour,
	Scale1Day:    time.Hour * 24,
	Scale1Week:   time.Hour * 24 * 7,
	// Month: how many days?
	// Year: is it a leap year?
}

// Duration is the interval size of one bucket of this scale.
func (s Scale) Duration() time.Duration {
	d, ok := scaleDurations[s]
	if !ok {
		panic(fmt.Sprintf("Unknown duration for scale %q", s))
	}
	return d
}

// scalePageSize is the duration that a single getChartUsage can span.
var scalePageSize = map[Scale]time.Duration{
	Scale1Second: 4000 * time.Second,
	Scale1Minute: 800 * time.Minute,
	Scale1Hour:   800 * time.Hour,
	// Week? Month? Year?
}

// PageSize is the maximum interval size that may be fetched with [Client.GetHistoryPage].
func (s Scale) PageSize() time.Duration {
	d, ok := scalePageSize[s]
	if !ok {
		panic(fmt.Sprintf("Unknown scale page size for scale %q", s))
	}
	return d
}
