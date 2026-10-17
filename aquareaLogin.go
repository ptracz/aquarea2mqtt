package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
)

// This gets tus through the entire login process, including populating string translation maps
func (aq *aquarea) aquareaSetup() bool {
	err := aq.aquareaLogin()
	if err != nil {
		log.Println(err)
		return false
	}

	err = aq.aquareaInstallerHome()
	if err != nil {
		log.Println(err)
		return false
	}

	aq.aquareaInitialFetch()

	return true
}

// first fetch of data and Home Assistant discovery
func (aq *aquarea) aquareaInitialFetch() {
	// populate internal data by feeding sub pages
	for _, user := range aq.usersMap {
		// Select this device in the session first
		if err := aq.selectDevice(user); err != nil {
			log.Println(err)
			continue
		}

		// Get settings from the device
		settings, err := aq.getDeviceSettings(user)
		if err != nil {
			log.Println(err)
		} else {
			// send HA configuration
			haConfig := aq.encodeSwitches(settings, user)
			aq.dataChannel <- haConfig
		}

		_, err = aq.parseDeviceStatus(user)
		if err != nil {
			log.Println(err)
		}
		// not using it for Home Assistant setup - at least for now

		settings, err = aq.getDeviceLogInformation(user)
		if err != nil {
			log.Println(err)
		} else {
			haConfig := aq.encodeSensors(settings, user)
			aq.dataChannel <- haConfig
		}
	}
}

func (aq *aquarea) aquareaLogin() error {
	data := []byte(aq.AquareaServiceCloudLogin + aq.AquareaServiceCloudPassword)
	b, err := aq.httpPost(aq.AquareaServiceCloudURL+"installer/api/auth/login", url.Values{
		"var.loginId":   {aq.AquareaServiceCloudLogin},
		"var.password":  {fmt.Sprintf("%x", md5.Sum(data))},
		"var.inputOmit": {"false"},
	})
	if err != nil {
		return err
	}

	var loginStruct aquareaLoginJSON
	err = json.Unmarshal(b, &loginStruct)

	if loginStruct.ErrorCode != 0 {
		err = fmt.Errorf("Aquarea login error code: %d", loginStruct.ErrorCode)
	}
	return err
}

func (aq *aquarea) aquareaInstallerHome() error {
	// GET installer home to initialize the session (SPA shell - nothing to parse from it)
	if _, err := aq.httpGet(aq.AquareaServiceCloudURL + "installer/home"); err != nil {
		return err
	}

	b, err := aq.httpPost(aq.AquareaServiceCloudURL+"/installer/api/endusers", url.Values{
		"var.name":      {""},
		"var.deviceId":  {""},
		"var.idu":       {""},
		"var.odu":       {""},
		"var.sortItem":  {"userName"},
		"var.sortOrder": {"0"},
		"var.offset":    {"0"},
		"var.limit":     {"999"},
		"var.mapSizeX":  {"0"},
		"var.mapSizeY":  {"0"},
		"var.readNew":   {"1"},
	})
	if err != nil {
		return err
	}
	var endUsersList aquareaEndUsersListJSON
	err = json.Unmarshal(b, &endUsersList)
	if err != nil {
		return err
	}
	if endUsersList.ErrorCode != 0 {
		return fmt.Errorf("endusers error code: %d", endUsersList.ErrorCode)
	}

	for _, user := range endUsersList.Endusers {
		aq.usersMap[user.Gwid] = user
	}

	if len(aq.usersMap) > 0 {
		aq.statusChannel <- true
	}
	return nil
}
