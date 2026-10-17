package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
)

// TODO: change to API and usec JSON for parsing responses instead of regex
func (aq *aquarea) sendSetting(cmd aquareaCommand) error {
	if cmd.value == "----" {
		log.Println("Dummy value - not sending to Aquarea Service Cloud")
		return nil
	}
	if len(aq.aquareaSettings.SettingsBackgroundData) == 0 {
		log.Println("Background data not received yet")
		// should not normally happen - we'll initialize this after log in, before main loop
		return nil
	}

	functionName := aq.reverseTranslation[cmd.setting]
	functionNamePOST := strings.ReplaceAll(functionName, "function-setting-user-select-", "userSelect")
	functionInfo := aq.translation[functionName]

	switch functionInfo.Kind {
	case "basic":
		//reverse translation from friendly name to xxxx-yyyy code and then to hex value
		crypticCode, ok := aq.reverseValueTrans[cmd.value]
		if !ok {
			return fmt.Errorf("no value translation for setting %s value %q - add it to valueTranslations in translation.json", cmd.setting, cmd.value)
		}
		cmd.value, ok = functionInfo.reverseValues[crypticCode]
		if !ok {
			return fmt.Errorf("no hex value for setting %s code %q - add it to values in translation.json", cmd.setting, crypticCode)
		}

	case "placeholder":
		i, _ := strconv.ParseInt(cmd.value, 0, 16)
		if !strings.Contains(cmd.setting, "HolidayMode") {
			// may be not true for all values...
			i += 128
		}
		cmd.value = fmt.Sprintf("0x%X", uint8(i))
	}

	user := aq.usersMap[cmd.deviceID]

	values := url.Values{
		"var.deviceId":            {user.DeviceID},
		"var.preOperation":        {aq.aquareaSettings.SettingsBackgroundData["0x80"].Value},
		"var.preMode":             {aq.aquareaSettings.SettingsBackgroundData["0xE0"].Value},
		"var.preTank":             {aq.aquareaSettings.SettingsBackgroundData["0xE1"].Value},
		"var." + functionNamePOST: {cmd.value},
	}

	log.Printf("Setting %s to %s on %s", cmd.setting, cmd.value, cmd.deviceID)

	_, err := aq.httpPost(aq.AquareaServiceCloudURL+"/installer/api/function/setting/user/set", values)
	//TODO parse output json, check error code
	//TODO usec check api to confirm settings are applied before returning
	return err
}

func (aq *aquarea) getDeviceSettings(user aquareaEndUserJSON) (map[string]string, error) {
	b, err := aq.httpPost(aq.AquareaServiceCloudURL+"/installer/api/function/setting/get", url.Values{
		"var.deviceId": {user.DeviceID},
	})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(b, &aq.aquareaSettings)
	if err != nil {
		return nil, err
	}
	if aq.aquareaSettings.ErrorCode != 0 {
		return nil, fmt.Errorf("settings error code: %d", aq.aquareaSettings.ErrorCode)
	}

	settings := make(map[string]string)

	for key, val := range aq.aquareaSettings.SettingDataInfo {
		if !strings.Contains(key, "user") {
			// not an user setting - ignoring
			continue
		}
		if _, ok := aq.translation[key]; ok {
			translation := aq.translation[key]
			var value string
			switch val.Type {
			case "basic-text":
				// not used in user settings
				value = aq.lookupValue(key, val.TextValue)
			case "select":
				switch translation.Kind {
				case "basic":
					value = aq.lookupValue(key, translation.Values[val.SelectedValue])

					// post possible values to a subtopic
					var allOptions string
					isFirst := true
					for _, option := range translation.Values {
						if !isFirst {
							allOptions += "\n"
						} else {
							isFirst = false
						}
						allOptions += aq.lookupValue(key, option)
					}
					settings[fmt.Sprintf("aquarea/%s/settings/%s/options", user.Gwid, translation.Name)] = allOptions
				case "placeholder":
					i, _ := strconv.ParseInt(val.SelectedValue, 0, 16)
					if !strings.Contains(translation.Name, "HolidayMode") {
						// might be not true for all values...
						i -= 128
					}
					value = strconv.FormatInt(int64(int8(i)), 10) // this is to get two's complement negatives right
				}
			case "placeholder-text":
				// not used in user settings, handling not correct
				value = val.Placeholder // + val.Params
			}
			settings[fmt.Sprintf("aquarea/%s/settings/%s", user.Gwid, translation.Name)] = value
		} else {
			aq.warnOnce("metadata:"+key, "No metadata in translation.json for: %s", key)
		}
	}
	return settings, err
}
