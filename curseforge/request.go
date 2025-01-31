package curseforge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// addonSlugRequest is sent to the CurseProxy GraphQL api to get the id from a slug
type addonSlugRequest struct {
	Query     string `json:"query"`
	Variables struct {
		Slug string `json:"slug"`
	} `json:"variables"`
	OperationName string `json:"operationName"`
}

// addonSlugResponse is received from the CurseProxy GraphQL api to get the id from a slug
type addonSlugResponse struct {
	Data struct {
		Addons []struct {
			ID              int `json:"id"`
			CategorySection struct {
				ID int `json:"id"`
			} `json:"categorySection"`
		} `json:"addons"`
	} `json:"data"`
	Exception  string   `json:"exception"`
	Message    string   `json:"message"`
	Stacktrace []string `json:"stacktrace"`
}

// Most of this is shamelessly copied from my previous attempt at modpack management:
// https://github.com/comp500/modpack-editor/blob/master/query.go
func modIDFromSlug(slug string) (int, error) {
	request := addonSlugRequest{
		Query: `
		query getIDFromSlug($slug: String) {
			addons(slug: $slug) {
				id
				categorySection {
					id
				}
			}
		}
		`,
		OperationName: "getIDFromSlug",
	}
	request.Variables.Slug = slug

	// Uses the curse.nikky.moe GraphQL api
	var response addonSlugResponse
	client := &http.Client{}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", "https://curse.nikky.moe/graphql", bytes.NewBuffer(requestBytes))
	if err != nil {
		return 0, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil && err != io.EOF {
		return 0, err
	}

	if len(response.Exception) > 0 || len(response.Message) > 0 {
		return 0, fmt.Errorf("error requesting id for slug: %s", response.Message)
	}

	for _, addonData := range response.Data.Addons {
		// Only use mods, not resource packs/modpacks
		if addonData.CategorySection.ID == 8 {
			return addonData.ID, nil
		}
	}
	return 0, errors.New("addon not found")
}

//noinspection GoUnusedConst
const (
	fileTypeRelease int = iota + 1
	fileTypeBeta
	fileTypeAlpha
)

//noinspection GoUnusedConst
const (
	dependencyTypeEmbedded int = iota + 1
	dependencyTypeOptional
	dependencyTypeRequired
	dependencyTypeTool
	dependencyTypeIncompatible
	dependencyTypeInclude
)

//noinspection GoUnusedConst
const (
	// modloaderTypeAny should not be passed to the API - it does not work
	modloaderTypeAny int = iota
	modloaderTypeForge
	modloaderTypeCauldron
	modloaderTypeLiteloader
	modloaderTypeFabric
)

//noinspection GoUnusedConst
const (
	hashAlgoSHA1 int = iota + 1
	hashAlgoMD5
)

// modInfo is a subset of the deserialised JSON response from the Curse API for mods (addons)
type modInfo struct {
	Name                   string        `json:"name"`
	Slug                   string        `json:"slug"`
	WebsiteURL             string        `json:"websiteUrl"`
	ID                     int           `json:"id"`
	LatestFiles            []modFileInfo `json:"latestFiles"`
	GameVersionLatestFiles []struct {
		// TODO: check how twitch launcher chooses which one to use, when you are on beta/alpha channel?!
		// or does it not have the concept of release channels?!
		GameVersion string `json:"gameVersion"`
		ID          int    `json:"projectFileId"`
		Name        string `json:"projectFileName"`
		FileType    int    `json:"fileType"`
		Modloader   int    `json:"modLoader"`
	} `json:"gameVersionLatestFiles"`
	ModLoaders []string `json:"modLoaders"`
}

func getModInfo(modID int) (modInfo, error) {
	var infoRes modInfo
	client := &http.Client{}

	idStr := strconv.Itoa(modID)

	req, err := http.NewRequest("GET", "https://addons-ecs.forgesvc.net/api/v2/addon/"+idStr, nil)
	if err != nil {
		return modInfo{}, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return modInfo{}, err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return modInfo{}, err
	}

	if infoRes.ID != modID {
		return modInfo{}, fmt.Errorf("unexpected addon ID in CurseForge response: %d/%d", modID, infoRes.ID)
	}

	return infoRes, nil
}

func getModInfoMultiple(modIDs []int) ([]modInfo, error) {
	var infoRes []modInfo
	client := &http.Client{}

	modIDsData, err := json.Marshal(modIDs)
	if err != nil {
		return []modInfo{}, err
	}

	req, err := http.NewRequest("POST", "https://addons-ecs.forgesvc.net/api/v2/addon/", bytes.NewBuffer(modIDsData))
	if err != nil {
		return []modInfo{}, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return []modInfo{}, err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return []modInfo{}, err
	}

	return infoRes, nil
}

const cfDateFormatString = "2006-01-02T15:04:05.999"

type cfDateFormat struct {
	time.Time
}

// Curse switched to proper RFC3339, but previously downloaded metadata still uses the old format :(
func (f *cfDateFormat) UnmarshalJSON(input []byte) error {
	trimmed := strings.Trim(string(input), `"`)
	timeValue, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		timeValue, err = time.Parse(cfDateFormatString, trimmed)
		if err != nil {
			return err
		}
	}

	f.Time = timeValue
	return nil
}

// modFileInfo is a subset of the deserialised JSON response from the Curse API for mod files
type modFileInfo struct {
	ID           int          `json:"id"`
	FileName     string       `json:"fileName"`
	FriendlyName string       `json:"displayName"`
	Date         cfDateFormat `json:"fileDate"`
	Length       int          `json:"fileLength"`
	FileType     int          `json:"releaseType"`
	// fileStatus? means latest/preferred?
	DownloadURL  string   `json:"downloadUrl"`
	GameVersions []string `json:"gameVersion"`
	Fingerprint  int      `json:"packageFingerprint"`
	Dependencies []struct {
		ModID int `json:"addonId"`
		Type  int `json:"type"`
	} `json:"dependencies"`

	Hashes []struct {
		Value     string `json:"value"`
		Algorithm int    `json:"algorithm"`
	} `json:"hashes"`
}

func (i modFileInfo) getBestHash() (hash string, hashFormat string) {
	// TODO: check if the hash is invalid (e.g. 0)
	hash = strconv.Itoa(i.Fingerprint)
	hashFormat = "murmur2"
	hashPreferred := 0

	// Prefer SHA1, then MD5 if found:
	if i.Hashes != nil {
		for _, v := range i.Hashes {
			if v.Algorithm == hashAlgoMD5 && hashPreferred < 1 {
				hashPreferred = 1

				hash = v.Value
				hashFormat = "md5"
			} else if v.Algorithm == hashAlgoSHA1 && hashPreferred < 2 {
				hashPreferred = 2

				hash = v.Value
				hashFormat = "sha1"
			}
		}
	}

	return
}

func getFileInfo(modID int, fileID int) (modFileInfo, error) {
	var infoRes modFileInfo
	client := &http.Client{}

	modIDStr := strconv.Itoa(modID)
	fileIDStr := strconv.Itoa(fileID)

	req, err := http.NewRequest("GET", "https://addons-ecs.forgesvc.net/api/v2/addon/"+modIDStr+"/file/"+fileIDStr, nil)
	if err != nil {
		return modFileInfo{}, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return modFileInfo{}, err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return modFileInfo{}, err
	}

	if infoRes.ID != fileID {
		return modFileInfo{}, fmt.Errorf("unexpected file ID in CurseForge response: %d/%d", modID, infoRes.ID)
	}

	return infoRes, nil
}

func getFileInfoMultiple(fileIDs []int) (map[string][]modFileInfo, error) {
	var infoRes map[string][]modFileInfo
	client := &http.Client{}

	modIDsData, err := json.Marshal(fileIDs)
	if err != nil {
		return make(map[string][]modFileInfo), err
	}

	req, err := http.NewRequest("POST", "https://addons-ecs.forgesvc.net/api/v2/addon/files", bytes.NewBuffer(modIDsData))
	if err != nil {
		return make(map[string][]modFileInfo), err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return make(map[string][]modFileInfo), err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return make(map[string][]modFileInfo), err
	}

	return infoRes, nil
}

func getSearch(searchText string, gameVersion string, modloaderType int) ([]modInfo, error) {
	var infoRes []modInfo
	client := &http.Client{}

	reqURL, err := url.Parse("https://addons-ecs.forgesvc.net/api/v2/addon/search?gameId=432&pageSize=10&categoryId=0&sectionId=6")
	if err != nil {
		return []modInfo{}, err
	}
	q := reqURL.Query()
	q.Set("searchFilter", searchText)

	if len(gameVersion) > 0 {
		q.Set("gameVersion", gameVersion)
	}
	if modloaderType != modloaderTypeAny {
		q.Set("modLoaderType", strconv.Itoa(modloaderType))
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", reqURL.String(), nil)
	if err != nil {
		return []modInfo{}, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return []modInfo{}, err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return []modInfo{}, err
	}

	return infoRes, nil
}

type addonFingerprintResponse struct {
	IsCacheBuilt bool `json:"isCacheBuilt"`
	ExactMatches []struct {
		ID          int           `json:"id"`
		File        modFileInfo   `json:"file"`
		LatestFiles []modFileInfo `json:"latestFiles"`
	} `json:"exactMatches"`
	ExactFingerprints        []int    `json:"exactFingerprints"`
	PartialMatches           []int    `json:"partialMatches"`
	PartialMatchFingerprints struct{} `json:"partialMatchFingerprints"`
	InstalledFingerprints    []int    `json:"installedFingerprints"`
	UnmatchedFingerprints    []int    `json:"unmatchedFingerprints"`
}

func getFingerprintInfo(hashes []int) (addonFingerprintResponse, error) {
	var infoRes addonFingerprintResponse
	client := &http.Client{}

	hashesData, err := json.Marshal(hashes)
	if err != nil {
		return addonFingerprintResponse{}, err
	}

	req, err := http.NewRequest("POST", "https://addons-ecs.forgesvc.net/api/v2/fingerprint", bytes.NewBuffer(hashesData))
	if err != nil {
		return addonFingerprintResponse{}, err
	}

	// TODO: make this configurable application-wide
	req.Header.Set("User-Agent", "packwiz/packwiz client")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return addonFingerprintResponse{}, err
	}

	err = json.NewDecoder(resp.Body).Decode(&infoRes)
	if err != nil && err != io.EOF {
		return addonFingerprintResponse{}, err
	}

	return infoRes, nil
}
