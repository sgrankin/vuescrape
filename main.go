package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"

	"sgrankin.dev/vuescrape/internal/jsondb"
	"sgrankin.dev/vuescrape/vmclient"
	"sgrankin.dev/vuescrape/vueclient"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
}

var (
	dest = flag.String("dest", "",
		"Destination host:port of VictoriaMetrics.")
	lookback = flag.Duration("lookback", 10*24*time.Hour,
		"Look this far back for measurements to catch up to what's in destination.")
	username = flag.String("username", "",
		"Emporia Vue username for initial auth.  Will be prompted if flag is not passed.")
	password = flag.String("passwod", "",
		"Emporia Vue passwod for initial auth.  Will be prompted if flag is not passed.")
)

func main() {
	flag.Parse()
	if err := run(*dest, *lookback, *username, *password); err != nil {
		log.Fatal(err)
	}
}

func run(dest string, lookback time.Duration, username, password string) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	vm := &vmclient.Client{Dest: url.URL{Scheme: "http", Host: dest}}
	tokDB, err := jsondb.Open[vueclient.Token](filepath.Join(configDir, "vuescrape", "auth.json"))
	if err != nil {
		return err
	}
	tok := vueclient.NewAtom(tokDB.Data)
	tok.Watch(func(t1, t2 *vueclient.Token) {
		tokDB.Data = t2
		if err := tokDB.Save(); err != nil {
			log.Fatalf("could not save new token: %v", err)
		}
	})
	vue := vueclient.NewClient(tok, func() (string, string, error) {
		if username != "" && password != "" {
			return username, password, nil
		}
		err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("username").Value(&username),
			huh.NewInput().Title("password").Password(true).Value(&password))).Run()
		return username, password, err
	})
	devs, err := vue.GetDevices()
	if err != nil {
		return err
	}
	until := time.Now()
	since := until.Add(-lookback)
	scale := vueclient.Scale1Minute
	for _, dev := range devs {
		for _, ch := range dev.Channels {
			if err := exportHistory(vm, vue, ch, since, until, scale); err != nil {
				return err
			}

		}
		for _, subdev := range dev.Devices {
			for _, ch := range subdev.Channels {
				if err := exportHistory(vm, vue, ch, since, until, scale); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// exportHistory will scrape the history for the given channel and push it to vm.
func exportHistory(vm *vmclient.Client, vue *vueclient.Client, ch vueclient.Channel, since, until time.Time, scale vueclient.Scale) error {
	// Find the last pushed change for this series so that we can advance `since`.
	seriesName := fmt.Sprintf("vue_kwh{dev_gid=%q,chan=%q,scale=%q}", fmt.Sprint(ch.DeviceGID), ch.ChannelNum, scale)
	existing, err := vm.Query(fmt.Sprintf("timestamp(%s[%s])", seriesName, until.Sub(since)))
	if err != nil {
		return err
	}
	if series, ok := existing.([]vmclient.Series); ok {
		if len(series) == 1 && len(series[0].Samples) == 1 {
			sample := series[0].Samples[0]
			// We expect 1 series (the one we asked) or none if it's not yet created.
			lastSample := time.Unix(int64(sample.Value), 0).
				// Add a scale interval so that we only get new samples and avoid writing duplicates.
				Add(scale.Duration())
			if lastSample.After(since) {
				since = lastSample
			}
		}
	}
	pusher, err := vm.Push()
	if err != nil {
		return err
	}
	defer pusher.Close()

	name := ch.Name
	if name == "" {
		name = "__total__"
	}
	series := vmclient.Series{
		Metric: vmclient.Metric{
			Name: "vue_kwh",
			Labels: map[string]string{
				"dev_gid":   fmt.Sprint(ch.DeviceGID),
				"chan":      ch.ChannelNum,
				"name":      ch.Name,
				"chan_mult": fmt.Sprint(ch.ChannelMultiplier),
				"scale":     string(scale),
			},
		},
	}

	start, found, err := vue.GetHistory(ch.DeviceGID, ch.ChannelNum, since, until, scale, vueclient.EnergyKWh)
	if err != nil {
		return err
	}
	c := 0
	for _, s := range found {
		ts := start
		start = start.Add(scale.Duration())
		if s == nil {
			continue
		}
		sample := vmclient.Sample{Value: *s, Timestamp: ts}
		series.Samples = append(series.Samples, sample)
		c++
		if len(series.Samples) > 1000 {
			pusher.Push(&series)
			series.Samples = nil
		}
	}
	if len(series.Samples) > 0 {
		pusher.Push(&series)
	}
	log.Printf("series %q found %d new samples", seriesName, c)
	return nil
}
