package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const translationFile = "translation.json"

// for passing MQTT set commands via channel
type aquareaCommand struct {
	deviceID string
	setting  string
	value    string
}
type aquareaFunctionDescription struct {
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Values        map[string]string `json:"values"`
	reverseValues map[string]string
}

type aquareaLogItem struct {
	Name   string
	Unit   string
	Values map[string]string
}

type aquarea struct {
	AquareaServiceCloudURL      string
	AquareaServiceCloudLogin    string
	AquareaServiceCloudPassword string
	logSecOffset                int64
	dataChannel                 chan map[string]string
	statusChannel               chan bool

	httpClient         http.Client
	valueTranslations  map[string]string                      // xxxx-yyyy codes translation to messages
	reverseValueTrans  map[string]string                      // message to xxxx-yyyy code translation
	usersMap           map[string]aquareaEndUserJSON          // list of users (devices) linked to an account
	translation        map[string]*aquareaFunctionDescription // function name meaning
	reverseTranslation map[string]string                      // map of friendly names to Aquarea meaningless ones
	logItems           []aquareaLogItem                       // table with names of log items (statistics view)
	warned             map[string]bool                        // warnings already logged, to avoid repeating them on every poll
	aquareaSettings    aquareaFunctionSettingGetJSON          // needs be cached, contains info relevant for changing settings
}

func aquareaHandler(ctx context.Context, wg *sync.WaitGroup, config configType, dataChannel chan map[string]string, commandChannel chan aquareaCommand, statusChannel chan bool) {
	defer wg.Done()
	log.Println("Starting Aquarea Service Cloud handler")
	var aquareaInstance aquarea
	aquareaInstance.AquareaServiceCloudURL = config.AquareaServiceCloudURL
	aquareaInstance.AquareaServiceCloudLogin = config.AquareaServiceCloudLogin
	aquareaInstance.AquareaServiceCloudPassword = config.AquareaServiceCloudPassword
	aquareaInstance.logSecOffset = config.LogSecOffset
	aquareaInstance.dataChannel = dataChannel
	aquareaInstance.statusChannel = statusChannel
	aquareaInstance.usersMap = make(map[string]aquareaEndUserJSON)

	aquareaInstance.loadTranslations(translationFile)

	poolInterval, err := time.ParseDuration(config.PoolInterval)
	if err != nil {
		log.Fatal(err)
	}

	timeout, err := time.ParseDuration(config.AquareaTimeout)
	if err != nil {
		log.Fatal(err)
	}
	cookieJar, _ := cookiejar.New(nil)
	aquareaInstance.httpClient = http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Jar:       cookieJar,
		Timeout:   timeout,
	}

	log.Println("Attempting to log in to Aquarea Service Cloud")
	for !aquareaInstance.aquareaSetup() {
	}
	log.Println("Logged in to Aquarea Service Cloud")

	ticker := time.NewTicker(poolInterval)
	for {
		select {
		case <-ticker.C:
			aquareaInstance.feedDataFromAquarea()
		case command := <-commandChannel:
			aquareaInstance.sendSetting(command)
		case <-ctx.Done():
			return
		}
	}
}

// translationLogItem is the per-index metadata for log items in translation.json
type translationLogItem struct {
	Name   string            `json:"name"`
	Unit   string            `json:"unit"`
	Values map[string]string `json:"values"`
}

func (aq *aquarea) loadTranslations(filename string) {
	// Load JSON with translations from Aquarea cryptic names
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	// translation.json holds function descriptions plus optional
	// valueTranslations and statisticsNames sections
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Fatal(err)
	}

	// cryptic xxxx-yyyy code -> human readable value name
	if rawValueTranslations, ok := raw["valueTranslations"]; ok {
		aq.valueTranslations = make(map[string]string)
		if err := json.Unmarshal(rawValueTranslations, &aq.valueTranslations); err != nil {
			log.Fatal(err)
		}
		// build reverse map for sending settings to the device
		aq.reverseValueTrans = make(map[string]string)
		for code, name := range aq.valueTranslations {
			aq.reverseValueTrans[name] = code
		}
	}

	// log items (statistics view) names and units, keyed by log index
	var statisticsNames map[string]translationLogItem
	if rawStatisticsNames, ok := raw["statisticsNames"]; ok {
		if err := json.Unmarshal(rawStatisticsNames, &statisticsNames); err != nil {
			log.Fatal(err)
		}
	}
	for idx, item := range statisticsNames {
		i, convErr := strconv.Atoi(idx)
		if convErr != nil {
			log.Fatalf("Invalid statistics index %q in translation.json", idx)
		}
		if i >= len(aq.logItems) {
			newItems := make([]aquareaLogItem, i+1)
			copy(newItems, aq.logItems)
			aq.logItems = newItems
		}
		aq.logItems[i] = aquareaLogItem{Name: item.Name, Unit: item.Unit, Values: item.Values}
	}
	// fill gaps so log index -> name lookup never misses
	for i := range aq.logItems {
		if aq.logItems[i].Name == "" {
			aq.logItems[i].Name = fmt.Sprintf("LogItem%d", i)
		}
	}
	if len(aq.logItems) == 0 {
		// no statistics metadata - use a default range with placeholder names
		// so log values can be discovered and named in translation.json
		log.Println("No statisticsNames in translation.json - using default log item indices 0-29 with placeholder names")
		aq.logItems = make([]aquareaLogItem, 30)
		for i := range aq.logItems {
			aq.logItems[i].Name = fmt.Sprintf("LogItem%d", i)
		}
	}

	// remove the non-function sections and unmarshal the rest as function descriptions
	delete(raw, "valueTranslations")
	delete(raw, "statisticsNames")
	cleanData, err := json.Marshal(raw)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(cleanData, &aq.translation); err != nil {
		log.Fatal(err)
	}

	// add reverse Value translation
	for kDescr, descr := range aq.translation {
		aq.translation[kDescr].reverseValues = make(map[string]string)
		for k, v := range descr.Values {
			descr.reverseValues[v] = k
		}
	}

	// create reverse map i.e. aquareaFunctionDescription.Name to key
	aq.reverseTranslation = make(map[string]string)
	for key, value := range aq.translation {
		if strings.Contains(key, "setting-user-select") {
			// can't do a reverse map of everything as there are duplicates
			// besides, we need it for settings only
			aq.reverseTranslation[value.Name] = key
		}
	}
}

// selectDevice sets the currently selected device in the cloud session.
// The data APIs (status, settings, log) only return data for the device
// selected this way.
func (aq *aquarea) selectDevice(user aquareaEndUserJSON) error {
	_, err := aq.httpPost(aq.AquareaServiceCloudURL+"/installer/functionUserInformation", url.Values{
		"var.functionSelectedGwUid": {user.GwUID},
	})
	return err
}

func (aq *aquarea) feedDataFromAquarea() {
	for _, user := range aq.usersMap {
		// Select this device in the session first
		if err := aq.selectDevice(user); err != nil {
			// most likely the session expired - mark offline and re-login
			aq.statusChannel <- false
			log.Println(err)
			log.Println("Will attempt to log in again")
			// try to log in again
			aq.aquareaSetup()
			continue
		}

		// Get settings from the device
		settings, err := aq.getDeviceSettings(user)
		if err != nil {
			log.Println(err)
		} else {
			aq.dataChannel <- settings
		}

		// Send device status
		deviceStatus, err := aq.parseDeviceStatus(user)
		if err != nil {
			log.Println(err)
		} else {
			aq.dataChannel <- deviceStatus
		}

		// Send device logs
		logData, err := aq.getDeviceLogInformation(user)
		if err != nil {
			log.Println(err)
		} else {
			aq.dataChannel <- logData
		}
	}
}

// lookupValue translates a cryptic value code (xxxx-yyyy) to a human readable
// name using valueTranslations. Unknown codes are logged once and returned as-is.
func (aq *aquarea) lookupValue(setting, code string) string {
	if code == "" {
		return ""
	}
	if name, ok := aq.valueTranslations[code]; ok {
		return name
	}
	aq.warnOnce("value:"+code, "No value translation for %s (code %q) - add it to valueTranslations in translation.json", setting, code)
	return code
}

// warnOnce logs a warning only the first time for a given key
func (aq *aquarea) warnOnce(key, format string, args ...interface{}) {
	if aq.warned == nil {
		aq.warned = make(map[string]bool)
	}
	if !aq.warned[key] {
		aq.warned[key] = true
		log.Printf(format, args...)
	}
}
