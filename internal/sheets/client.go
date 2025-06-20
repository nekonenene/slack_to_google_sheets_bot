package sheets

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

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

	// Check for duplicates by looking for existing message ID
	if exists, err := c.messageExistsInSheet(spreadsheetID, sheetName, record.MessageTS); err != nil {
		log.Printf("Warning: could not check for duplicates: %v", err)
	} else if exists {
		log.Printf("Message %s already exists in sheet %s, skipping", record.MessageTS, sheetName)
		return nil
	}

	// Get the next row number (No.)
	nextRowNumber, err := c.getNextRowNumber(spreadsheetID, sheetName)
	if err != nil {
		log.Printf("Warning: could not get next row number: %v", err)
		nextRowNumber = 1 // Default to 1 if we can't determine
	}

	// Find thread parent No. if this is a thread reply
	threadParentNo := ""
	if record.ThreadTS != "" && record.ThreadTS != record.MessageTS {
		if parentNo, err := c.findThreadParentNo(spreadsheetID, sheetName, record.ThreadTS); err != nil {
			log.Printf("Warning: could not find thread parent: %v", err)
		} else if parentNo > 0 {
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
	headers := []interface{}{
		"No.",
		"投稿日時",
		"発信者（ハンドル名）",
		"発信者（本名）",
		"発言内容",
		"どの No. のスレッド投稿に対する投稿か（スレッドに紐づく投稿でなければ空白）",
		"投稿ID",
	}

	headerRange := &sheets.ValueRange{
		Values: [][]interface{}{headers},
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
	headers := []interface{}{
		"No.",
		"投稿日時",
		"発信者（ハンドル名）",
		"発信者（本名）",
		"発言内容",
		"どの No. のスレッド投稿に対する投稿か（スレッドに紐づく投稿でなければ空白）",
		"投稿ID",
	}

	headerRange := &sheets.ValueRange{
		Values: [][]interface{}{headers},
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
