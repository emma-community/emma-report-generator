package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	emmaSdk "github.com/emma-community/emma-go-sdk"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Credential struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

type ErrorResponse struct {
	Message string `json:"message"`
}

func main() {
	http.HandleFunc("/v1/vm-reports", generateCSVHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func generateCSVHandler(w http.ResponseWriter, r *http.Request) {
	credentialsEnv := os.Getenv("CREDENTIALS")
	credPairs := strings.Split(credentialsEnv, ",")

	var credentials []Credential
	for _, pair := range credPairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			credentials = append(credentials, Credential{ClientId: parts[0], ClientSecret: parts[1]})
		}
	}

	apiClient := emmaSdk.NewAPIClient(emmaSdk.NewConfiguration())

	filenames, err := processCredentials(apiClient, credentials)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filename, err := combineCsvFiles(filenames)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Error combining CSV files, %s", err.Error()))
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=%s", filename))
	w.Header().Set("Content-Type", "text/csv")
	http.ServeFile(w, r, filename)
	os.Remove(filename)
}

func processCredentials(apiClient *emmaSdk.APIClient, credentials []Credential) ([]string, error) {
	var filenames []string
	for _, cred := range credentials {
		token, err := getToken(apiClient, cred.ClientId, cred.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("unauthorized: %v", err)
		}

		projectId, companyId, err := extractDataFromToken(token)
		if err != nil {
			return nil, fmt.Errorf("invalid token: %v", err)
		}

		vmsData, err := fetchVmsData(apiClient, token)
		if err != nil {
			return nil, fmt.Errorf("error fetching VMs data: %v", err)
		}

		if len(vmsData) == 0 {
			continue
		}

		filename := fmt.Sprintf("temp_report_%s_%s_%s.csv", companyId, projectId, time.Now().UTC().Format(time.RFC3339))
		filenames = append(filenames, filename)

		if err := writeCsvFile(filename, vmsData); err != nil {
			return nil, fmt.Errorf("could not create file: %s", err.Error())
		}
	}
	return filenames, nil
}

func writeCsvFile(filename string, vmsData []map[string]interface{}) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headersMap := make(map[string]struct{})
	var headers []string
	var flattenedDataArr []map[string]string

	for _, vmData := range vmsData {
		flattenedData := make(map[string]string)
		flattenJSON(vmData, "", flattenedData)
		flattenedDataArr = append(flattenedDataArr, flattenedData)

		for key := range flattenedData {
			if _, exists := headersMap[key]; !exists {
				headersMap[key] = struct{}{}
				headers = append(headers, key)
			}
		}
	}

	writer.Write(headers)
	for _, flattenedData := range flattenedDataArr {
		row := make([]string, len(headers))
		for i, header := range headers {
			row[i] = flattenedData[header]
		}
		writer.Write(row)
	}

	return writer.Error()
}

func flattenJSON(data interface{}, prefix string, result map[string]string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			flattenJSON(val, prefix+k+".", result)
		}
	case []interface{}:
		for i, val := range v {
			flattenJSON(val, fmt.Sprintf("%s%d.", prefix, i), result)
		}
	default:
		if prefix != "" {
			result[prefix[:len(prefix)-1]] = fmt.Sprintf("%v", data)
		} else {
			result[prefix] = fmt.Sprintf("%v", data)
		}
	}
}

func getToken(apiClient *emmaSdk.APIClient, clientId string, clientSecret string) (string, error) {
	credentials := emmaSdk.Credentials{ClientId: clientId, ClientSecret: clientSecret}
	token, resp, err := apiClient.AuthenticationAPI.IssueToken(context.Background()).Credentials(credentials).Execute()

	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("error fetching token: %v", string(body))
	}

	return *token.AccessToken, nil
}

func extractDataFromToken(tokenString string) (string, string, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", "", err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("invalid token claims")
	}

	isExternalApplication, ok := claims["isExternalApplication"].(bool)

	if !ok {
		return "", "", fmt.Errorf("isExternalApplication not found in token")
	}

	if !isExternalApplication {
		return "", "", fmt.Errorf("not correct client credentials")
	}

	projectId, ok := claims["projectId"].(float64)
	if !ok {
		return "", "", fmt.Errorf("projectId not found in token")
	}

	companyId, ok := claims["companyId"].(float64)
	if !ok {
		return "", "", fmt.Errorf("companyId not found in token")
	}

	return fmt.Sprint(projectId), fmt.Sprint(companyId), nil
}

func fetchVmsData(apiClient *emmaSdk.APIClient, token string) ([]map[string]interface{}, error) {
	auth := context.WithValue(context.Background(), emmaSdk.ContextAccessToken, token)

	vms, resp, err := apiClient.VirtualMachinesAPI.GetVms(auth).Execute()

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error fetching vms: %v", string(body))
	}

	result := make([]map[string]interface{}, 0)
	for _, vm := range vms {
		vmJson, err := json.Marshal(vm)
		if err != nil {
			return nil, fmt.Errorf("error converting vms to JSON: %s", err)
		}
		var resultMap map[string]interface{}
		err = json.Unmarshal(vmJson, &resultMap)
		if err != nil {
			return nil, fmt.Errorf("error unmarshaling JSON to map: %s", err)
		}
		result = append(result, resultMap)
	}
	return result, nil
}

func combineCsvFiles(filenames []string) (string, error) {
	headersFileMap, headersUniqueMap := collectHeaders(filenames)

	headers := make([]string, 0)
	for key := range headersUniqueMap {
		headers = append(headers, key)
	}

	resultFilename := fmt.Sprintf("report_%s.csv", time.Now().UTC().Format(time.RFC3339))
	combinedFile, err := os.Create(resultFilename)
	if err != nil {
		return "", err
	}
	defer combinedFile.Close()

	writer := csv.NewWriter(combinedFile)
	defer writer.Flush()
	writer.Write(headers)

	for _, filename := range filenames {
		if err := writeRowsFromFiles(writer, filename, headers, headersFileMap[filename]); err != nil {
			return "", err
		}
		os.Remove(filename)
	}
	return resultFilename, nil
}

func collectHeaders(filenames []string) (map[string]map[string]int, map[string]struct{}) {
	headersFileMap := make(map[string]map[string]int)
	headersUniqueMap := make(map[string]struct{})
	for _, filename := range filenames {
		file, err := os.Open(filename)
		if err != nil {
			continue
		}
		headers, err := readCSVHeaders(file)
		if err != nil {
			continue
		}
		headersMap := make(map[string]int)
		for i, header := range headers {
			headersMap[header] = i
			headersUniqueMap[header] = struct{}{}
		}
		headersFileMap[filename] = headersMap
	}
	return headersFileMap, headersUniqueMap
}

func writeRowsFromFiles(writer *csv.Writer, filename string, headers []string, headersMap map[string]int) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	rows, err := readCSVRows(file)
	if err != nil {
		return err
	}

	for _, row := range rows {
		combinedRow := make([]string, len(headers))
		for i, header := range headers {
			if idx, exists := headersMap[header]; exists {
				combinedRow[i] = row[idx]
			}
		}
		writer.Write(combinedRow)
	}
	return nil
}

func readCSVHeaders(file *os.File) ([]string, error) {
	reader := csv.NewReader(file)
	return reader.Read()
}

func readCSVRows(file *os.File) ([][]string, error) {
	reader := csv.NewReader(file)
	_, err := reader.Read() // Skip headers
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Message: message})
}
