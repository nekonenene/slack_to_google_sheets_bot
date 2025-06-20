package sheets

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Expected headers for Google Sheets
var expectedHeaders = []interface{}{
	"No.",
	"投稿日時",
	"発信者（ハンドル名）",
	"発信者（本名）",
	"発言内容",
	"どの No. のスレッド投稿に対する投稿か（スレッドに紐づく投稿でなければ空白）",
	"投稿ID",
}

type Client struct {
	service *sheets.Service
}

func NewClient(credentialsJSON string) (*Client, error) {
	ctx := context.Background()

	var credentialsData []byte
	var err error

	// Check if credentialsJSON is a file path or JSON content
	// File path criteria: shorter than 512 chars, ends with .json, and doesn't start with {
	isFilePath := len(credentialsJSON) < 512 &&
		strings.HasSuffix(credentialsJSON, ".json") &&
		!strings.HasPrefix(strings.TrimSpace(credentialsJSON), "{")

	if isFilePath {
		// It's likely a file path, try to read the file
		credentialsData, err = os.ReadFile(credentialsJSON)
		if err != nil {
			return nil, fmt.Errorf("unable to read credentials file '%s': %v", credentialsJSON, err)
		}
		log.Printf("Read credentials from file: %s (%d bytes)", credentialsJSON, len(credentialsData))
	} else {
		// It's JSON content
		credentialsData = []byte(credentialsJSON)
		log.Printf("Using credentials as JSON content (%d bytes)", len(credentialsData))
	}

	service, err := sheets.NewService(ctx, option.WithCredentialsJSON(credentialsData))
	if err != nil {
		return nil, fmt.Errorf("unable to create sheets service: %v", err)
	}

	return &Client{service: service}, nil
}

const maxRetryAttempts = 4

// retryWithBackoff executes a function with exponential backoff retry logic
func retryWithBackoff(operation func() error, description string) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			if attempt > 1 {
				log.Printf("Retry successful for %s on attempt %d", description, attempt)
			}
			return nil
		}

		log.Printf("Attempt %d failed for %s: %v", attempt, description, lastErr)

		// If this was the last attempt, don't sleep
		if attempt == maxRetryAttempts {
			break
		}

		// Sleep for attempt seconds (1s, 2s, 3s)
		delay := time.Duration(attempt) * time.Second
		log.Printf("Retrying %s in %v (attempt %d)...", description, delay, attempt+1)
		time.Sleep(delay)
	}

	log.Printf("All retry attempts failed for %s. Final error: %v", description, lastErr)
	return lastErr
}

type MessageRecord struct {
	Timestamp    time.Time
	Channel      string
	ChannelName  string
	User         string
	UserHandle   string
	UserRealName string
	Text         string
	ThreadTS     string
	MessageTS    string
}

func (c *Client) WriteMessage(spreadsheetID string, record *MessageRecord) error {
	// Determine sheet name: "ChannelName-ChannelID"
	sheetName := fmt.Sprintf("%s-%s", record.ChannelName, record.Channel)

	// Ensure sheet exists (handles creation and name updates)
	if err := c.ensureChannelSheetExists(spreadsheetID, record.Channel, record.ChannelName); err != nil {
		return err
	}

	// Get sheet data once for all operations (efficiency)
	sheetData, err := c.getSheetData(spreadsheetID, sheetName)
	if err != nil {
		return fmt.Errorf("failed to get sheet data: %v", err)
	}

	// Check and fix header if needed
	if err := c.ensureCorrectHeader(spreadsheetID, sheetName, sheetData); err != nil {
		log.Printf("Warning: could not ensure correct header: %v", err)
		// Reload data after header fix
		sheetData, err = c.getSheetData(spreadsheetID, sheetName)
		if err != nil {
			return fmt.Errorf("failed to reload sheet data after header fix: %v", err)
		}
	}

	// Check for duplicates using already loaded data
	if c.messageExistsInData(sheetData, record.MessageTS) {
		log.Printf("Message %s already exists in sheet %s, skipping", record.MessageTS, sheetName)
		return nil
	}

	// Get the next row number (No.) from loaded data
	nextRowNumber := c.getNextRowNumberFromData(sheetData)

	// Find thread parent No. if this is a thread reply using loaded data
	threadParentNo := ""
	if record.ThreadTS != "" && record.ThreadTS != record.MessageTS {
		if parentNo := c.findThreadParentNoInData(sheetData, record.ThreadTS); parentNo > 0 {
			threadParentNo = fmt.Sprintf("%d", parentNo)
		}
	}

	values := []interface{}{
		nextRowNumber,
		record.Timestamp.Format("2006-01-02 15:04:05"),
		record.UserHandle,
		record.UserRealName,
		record.Text,
		threadParentNo,
		record.MessageTS,
	}

	// Append the row
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err = c.service.Spreadsheets.Values.Append(
		spreadsheetID,
		sheetName+"!A:G",
		valueRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		return fmt.Errorf("unable to write data to sheet: %v", err)
	}

	return nil
}

func (c *Client) messageExistsInSheet(spreadsheetID, sheetName, messageTS string) (bool, error) {
	// Get all message IDs from column G in the specific sheet
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!G:G").Do()
	if err != nil {
		return false, err
	}

	for _, row := range resp.Values {
		if len(row) > 0 && row[0] == messageTS {
			return true, nil
		}
	}

	return false, nil
}

func (c *Client) ensureSheetExists(spreadsheetID, sheetName string) error {
	// Get spreadsheet info
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("unable to get spreadsheet: %v", err)
	}

	// Check if sheet exists
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			return nil // Sheet exists
		}
	}

	// Create the sheet
	requests := []*sheets.Request{
		{
			AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{
					Title: sheetName,
				},
			},
		},
	}

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}

	_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
	if err != nil {
		return fmt.Errorf("unable to create sheet: %v", err)
	}

	// Add headers

	headerRange := &sheets.ValueRange{
		Values: [][]interface{}{expectedHeaders},
	}

	_, err = c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		sheetName+"!A1:G1",
		headerRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		log.Printf("Warning: unable to add headers: %v", err)
	}

	return nil
}

func (c *Client) EnsureSheetExists(spreadsheetID, sheetName string) error {
	return c.ensureSheetExists(spreadsheetID, sheetName)
}

func (c *Client) EnsureChannelSheetExists(spreadsheetID, channelID, channelName string) error {
	return c.ensureChannelSheetExists(spreadsheetID, channelID, channelName)
}

func (c *Client) ensureChannelSheetExists(spreadsheetID, channelID, channelName string) error {
	// Get spreadsheet info
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("unable to get spreadsheet: %v", err)
	}

	expectedSheetName := fmt.Sprintf("%s-%s", channelName, channelID)
	var existingSheet *sheets.Sheet
	var sheetToRename *sheets.Sheet

	// Look for existing sheets
	for _, sheet := range spreadsheet.Sheets {
		sheetTitle := sheet.Properties.Title

		// Check if sheet name ends with the channel ID (exact match)
		if strings.HasSuffix(sheetTitle, "-"+channelID) {
			existingSheet = sheet
			// Check if the name needs updating
			if sheetTitle != expectedSheetName {
				sheetToRename = sheet
			}
			break
		}
	}

	// If sheet exists and name needs updating
	if sheetToRename != nil {
		log.Printf("Updating sheet name from '%s' to '%s'", sheetToRename.Properties.Title, expectedSheetName)

		updateRequest := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
						Properties: &sheets.SheetProperties{
							SheetId: sheetToRename.Properties.SheetId,
							Title:   expectedSheetName,
						},
						Fields: "title",
					},
				},
			},
		}

		_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, updateRequest).Do()
		if err != nil {
			return fmt.Errorf("unable to rename sheet: %v", err)
		}

		log.Printf("Sheet renamed successfully to '%s'", expectedSheetName)
		return nil
	}

	// If sheet already exists with correct name
	if existingSheet != nil {
		return nil
	}

	// Create new sheet
	log.Printf("Creating new sheet: '%s'", expectedSheetName)

	createRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{
						Title: expectedSheetName,
					},
				},
			},
		},
	}

	_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, createRequest).Do()
	if err != nil {
		return fmt.Errorf("unable to create sheet: %v", err)
	}

	// Add headers to new sheet

	headerRange := &sheets.ValueRange{
		Values: [][]interface{}{expectedHeaders},
	}

	_, err = c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		expectedSheetName+"!A1:G1",
		headerRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		log.Printf("Warning: unable to add headers to new sheet: %v", err)
	}

	log.Printf("Sheet created successfully: '%s'", expectedSheetName)
	return nil
}

func (c *Client) getNextRowNumber(spreadsheetID, sheetName string) (int, error) {
	// Get all data to count existing rows
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!A:A").Do()
	if err != nil {
		return 1, err
	}

	// Count rows (subtract 1 for header row, then add 1 for next number)
	rowCount := len(resp.Values)
	if rowCount <= 1 {
		return 1, nil // First data row after header
	}

	return rowCount, nil // This gives us the next row number
}

func (c *Client) findThreadParentNo(spreadsheetID, sheetName, threadTS string) (int, error) {
	// Get message timestamps (column G) and row numbers (column A)
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!A:G").Do()
	if err != nil {
		return 0, err
	}

	// Skip header row (index 0) and search for the thread parent
	for i, row := range resp.Values {
		if i == 0 {
			continue // Skip header
		}

		if len(row) >= 7 && row[6] == threadTS {
			// Found the parent message, return its No. (column A)
			if len(row) >= 1 {
				if rowNo, ok := row[0].(float64); ok {
					return int(rowNo), nil
				}
				if rowNoStr, ok := row[0].(string); ok {
					if rowNo, err := strconv.Atoi(rowNoStr); err == nil {
						return rowNo, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("thread parent not found")
}

func (c *Client) getSheetData(spreadsheetID, sheetName string) (*sheets.ValueRange, error) {
	// Get all data from the sheet in one API call
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!A:G").Do()
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ensureCorrectHeader(spreadsheetID, sheetName string, sheetData *sheets.ValueRange) error {

	// Check if header exists and is correct
	needsHeaderUpdate := false
	if len(sheetData.Values) == 0 {
		needsHeaderUpdate = true
		log.Printf("Sheet %s has no data, adding header", sheetName)
	} else {
		headerRow := sheetData.Values[0]
		if len(headerRow) != len(expectedHeaders) {
			needsHeaderUpdate = true
			log.Printf("Sheet %s header has wrong number of columns: got %d, expected %d",
				sheetName, len(headerRow), len(expectedHeaders))
		} else {
			for i, expected := range expectedHeaders {
				if i >= len(headerRow) || headerRow[i] != expected {
					needsHeaderUpdate = true
					log.Printf("Sheet %s header column %d incorrect: got '%v', expected '%v'",
						sheetName, i+1, headerRow[i], expected)
					break
				}
			}
		}
	}

	if needsHeaderUpdate {
		log.Printf("Updating header for sheet %s", sheetName)
		headerRange := &sheets.ValueRange{
			Values: [][]interface{}{expectedHeaders},
		}

		_, err := c.service.Spreadsheets.Values.Update(
			spreadsheetID,
			sheetName+"!A1:G1",
			headerRange,
		).ValueInputOption("RAW").Do()

		if err != nil {
			return fmt.Errorf("failed to update header: %v", err)
		}
		log.Printf("Header updated successfully for sheet %s", sheetName)
	}

	return nil
}

func (c *Client) messageExistsInData(sheetData *sheets.ValueRange, messageTS string) bool {
	// Skip header row (index 0) and check message IDs in column G (index 6)
	for i, row := range sheetData.Values {
		if i == 0 {
			continue // Skip header
		}
		if len(row) > 6 && row[6] == messageTS {
			return true
		}
	}
	return false
}

func (c *Client) getNextRowNumberFromData(sheetData *sheets.ValueRange) int {
	// Count rows (subtract 1 for header row, then add 1 for next number)
	rowCount := len(sheetData.Values)
	if rowCount <= 1 {
		return 1 // First data row after header
	}
	return rowCount // This gives us the next row number
}

func (c *Client) findThreadParentNoInData(sheetData *sheets.ValueRange, threadTS string) int {
	// Skip header row (index 0) and search for the thread parent
	for i, row := range sheetData.Values {
		if i == 0 {
			continue // Skip header
		}

		if len(row) >= 7 && row[6] == threadTS {
			// Found the parent message, return its No. (column A)
			if len(row) >= 1 {
				if rowNo, ok := row[0].(float64); ok {
					return int(rowNo)
				}
				if rowNoStr, ok := row[0].(string); ok {
					if rowNo, err := strconv.Atoi(rowNoStr); err == nil {
						return rowNo
					}
				}
			}
		}
	}
	return 0
}

func (c *Client) ClearSheetData(spreadsheetID, sheetName string) error {
	// Get sheet properties to find the sheet ID
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("unable to get spreadsheet: %v", err)
	}

	var sheetID int64
	found := false
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			sheetID = sheet.Properties.SheetId
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("sheet %s not found", sheetName)
	}

	// Clear all data except headers (row 2 onwards)
	requests := []*sheets.Request{
		{
			DeleteDimension: &sheets.DeleteDimensionRequest{
				Range: &sheets.DimensionRange{
					SheetId:    sheetID,
					Dimension:  "ROWS",
					StartIndex: 1, // Start from row 2 (0-indexed, so 1 = row 2)
				},
			},
		},
	}

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}

	_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
	if err != nil {
		return fmt.Errorf("unable to clear sheet data: %v", err)
	}

	log.Printf("Cleared all data from sheet %s (keeping headers)", sheetName)
	return nil
}

func (c *Client) GetLatestTimestamp(spreadsheetID, sheetName string) (string, error) {
	var latestTS string
	err := retryWithBackoff(func() error {
		// Get all data from timestamp column (G)
		resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!G:G").Do()
		if err != nil {
			return err
		}

		// Find the latest (largest) timestamp, skipping header
		for i, row := range resp.Values {
			if i == 0 {
				continue // Skip header row
			}
			if len(row) > 0 && row[0] != nil {
				if ts, ok := row[0].(string); ok && ts != "" {
					if latestTS == "" || ts > latestTS {
						latestTS = ts
					}
				}
			}
		}

		return nil
	}, fmt.Sprintf("get latest timestamp from sheet %s", sheetName))

	if err != nil {
		return "", err
	}

	log.Printf("Latest timestamp in sheet %s: %s", sheetName, latestTS)
	return latestTS, nil
}

func (c *Client) WriteMessagesStreaming(spreadsheetID string, records []*MessageRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Use the first record to determine sheet name (all should be same channel)
	sheetName := fmt.Sprintf("%s-%s", records[0].ChannelName, records[0].Channel)

	// Ensure sheet exists
	if err := c.ensureChannelSheetExists(spreadsheetID, records[0].Channel, records[0].ChannelName); err != nil {
		return err
	}

	// Get existing sheet data once
	sheetData, err := c.getSheetData(spreadsheetID, sheetName)
	if err != nil {
		return fmt.Errorf("failed to get sheet data: %v", err)
	}

	// Check and fix header if needed
	if err := c.ensureCorrectHeader(spreadsheetID, sheetName, sheetData); err != nil {
		log.Printf("Warning: could not ensure correct header: %v", err)
		// Reload data after header fix
		sheetData, err = c.getSheetData(spreadsheetID, sheetName)
		if err != nil {
			return fmt.Errorf("failed to reload sheet data after header fix: %v", err)
		}
	}

	// Filter out duplicate messages
	var newRecords []*MessageRecord
	for _, record := range records {
		if !c.messageExistsInData(sheetData, record.MessageTS) {
			newRecords = append(newRecords, record)
		}
	}

	if len(newRecords) == 0 {
		log.Printf("All %d messages already exist in sheet %s, skipping batch", len(records), sheetName)
		return nil
	}

	// Sort new records by timestamp (should already be sorted from search API)
	sort.Slice(newRecords, func(i, j int) bool {
		return newRecords[i].Timestamp.Before(newRecords[j].Timestamp)
	})

	// Get starting row number
	startRowNumber := c.getNextRowNumberFromData(sheetData)

	// Prepare values for batch insert
	var values [][]interface{}
	for i, record := range newRecords {
		rowNumber := startRowNumber + i

		// Find thread parent No. if this is a thread reply
		threadParentNo := ""
		if record.ThreadTS != "" && record.ThreadTS != record.MessageTS {
			// Check in existing data first
			if parentNo := c.findThreadParentNoInData(sheetData, record.ThreadTS); parentNo > 0 {
				threadParentNo = fmt.Sprintf("%d", parentNo)
			} else {
				// Check in the current batch being processed
				for j := 0; j < i; j++ {
					if newRecords[j].MessageTS == record.ThreadTS {
						threadParentNo = fmt.Sprintf("%d", startRowNumber+j)
						break
					}
				}
			}
		}

		values = append(values, []interface{}{
			rowNumber,
			record.Timestamp.Format("2006-01-02 15:04:05"),
			record.UserHandle,
			record.UserRealName,
			record.Text,
			threadParentNo,
			record.MessageTS,
		})
	}

	// Write batch to sheet
	if len(values) > 0 {
		err := retryWithBackoff(func() error {
			valueRange := &sheets.ValueRange{
				Values: values,
			}

			_, err := c.service.Spreadsheets.Values.Append(
				spreadsheetID,
				sheetName+"!A:G",
				valueRange,
			).ValueInputOption("RAW").Do()

			return err
		}, fmt.Sprintf("stream write %d messages to sheet %s", len(values), sheetName))

		if err != nil {
			return fmt.Errorf("unable to stream write data to sheet: %v", err)
		}

		log.Printf("Successfully streamed %d new messages to sheet %s (filtered %d duplicates)", 
			len(values), sheetName, len(records)-len(newRecords))
	}

	return nil
}

func (c *Client) WriteBatchMessages(spreadsheetID string, records []*MessageRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Sort records by timestamp (oldest first)
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})

	// Use the first record to determine sheet name (all should be same channel)
	sheetName := fmt.Sprintf("%s-%s", records[0].ChannelName, records[0].Channel)

	// Ensure sheet exists
	if err := c.ensureChannelSheetExists(spreadsheetID, records[0].Channel, records[0].ChannelName); err != nil {
		return err
	}

	// Get existing sheet data
	sheetData, err := c.getSheetData(spreadsheetID, sheetName)
	if err != nil {
		return fmt.Errorf("failed to get sheet data: %v", err)
	}

	// Check and fix header if needed
	if err := c.ensureCorrectHeader(spreadsheetID, sheetName, sheetData); err != nil {
		log.Printf("Warning: could not ensure correct header: %v", err)
		// Reload data after header fix
		sheetData, err = c.getSheetData(spreadsheetID, sheetName)
		if err != nil {
			return fmt.Errorf("failed to reload sheet data after header fix: %v", err)
		}
	}

	// Filter out duplicate messages
	var newRecords []*MessageRecord
	for _, record := range records {
		if !c.messageExistsInData(sheetData, record.MessageTS) {
			newRecords = append(newRecords, record)
		}
	}

	if len(newRecords) == 0 {
		log.Printf("All messages already exist in sheet %s, nothing to add", sheetName)
		return nil
	}

	// Prepare values for batch insert
	var values [][]interface{}
	startRowNumber := c.getNextRowNumberFromData(sheetData)

	for i, record := range newRecords {
		rowNumber := startRowNumber + i

		// Find thread parent No. if this is a thread reply
		threadParentNo := ""
		if record.ThreadTS != "" && record.ThreadTS != record.MessageTS {
			// Check in existing data first
			if parentNo := c.findThreadParentNoInData(sheetData, record.ThreadTS); parentNo > 0 {
				threadParentNo = fmt.Sprintf("%d", parentNo)
			} else {
				// Check in the current batch being processed
				for j := 0; j < i; j++ {
					if newRecords[j].MessageTS == record.ThreadTS {
						threadParentNo = fmt.Sprintf("%d", startRowNumber+j)
						break
					}
				}
			}
		}

		values = append(values, []interface{}{
			rowNumber,
			record.Timestamp.Format("2006-01-02 15:04:05"),
			record.UserHandle,
			record.UserRealName,
			record.Text,
			threadParentNo,
			record.MessageTS,
		})
	}

	// Batch insert all new messages
	if len(values) > 0 {
		err := retryWithBackoff(func() error {
			valueRange := &sheets.ValueRange{
				Values: values,
			}

			_, err := c.service.Spreadsheets.Values.Append(
				spreadsheetID,
				sheetName+"!A:G",
				valueRange,
			).ValueInputOption("RAW").Do()

			return err
		}, fmt.Sprintf("write %d messages to sheet %s", len(values), sheetName))

		if err != nil {
			return fmt.Errorf("unable to write batch data to sheet: %v", err)
		}

		log.Printf("Successfully wrote %d messages to sheet %s in chronological order", len(values), sheetName)
	}

	return nil
}
