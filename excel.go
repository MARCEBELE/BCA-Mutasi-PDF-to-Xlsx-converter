package main

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// ExportToExcel creates an Excel file with parsed data
func (p *BCAParser) ExportToExcel(filename string) error {
	f := excelize.NewFile()

	// Set document author / creator metadata
	f.SetAppProps(&excelize.AppProperties{
		Application: "BCA Statement Converter",
		Company:     "MARCEBELE",
	})
	f.SetDocProps(&excelize.DocProperties{
		Creator:        "MARCEBELE",
		LastModifiedBy: "MARCEBELE",
	})

	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	// Create sheets
	f.SetSheetName("Sheet1", "Account Info")
	_, err := f.NewSheet("Transactions")
	if err != nil {
		return err
	}
	_, err = f.NewSheet("Summary")
	if err != nil {
		return err
	}

	// Populate Account Info sheet
	err = p.createAccountInfoSheet(f)
	if err != nil {
		return err
	}

	// Populate Transactions sheet
	err = p.createTransactionsSheet(f)
	if err != nil {
		return err
	}

	// Populate Summary sheet
	err = p.createSummarySheet(f)
	if err != nil {
		return err
	}

	// Set active sheet
	f.SetActiveSheet(1) // Transactions sheet

	// Save file
	if err := f.SaveAs(filename); err != nil {
		return err
	}

	return nil
}

// createAccountInfoSheet creates the Account Info sheet
func (p *BCAParser) createAccountInfoSheet(f *excelize.File) error {
	sheet := "Account Info"

	// Headers
	headers := []string{"Account Number", "Period", "Account Holder", "Currency"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Data
	data := []interface{}{
		p.AccountInfo.AccountNumber,
		p.AccountInfo.Period,
		p.AccountInfo.AccountHolder,
		p.AccountInfo.Currency,
	}
	for i, value := range data {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		f.SetCellValue(sheet, cell, value)
	}

	// Apply header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	for i := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Auto-fit columns
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, 20)
	}

	return nil
}

// createTransactionsSheet creates the Transactions sheet
func (p *BCAParser) createTransactionsSheet(f *excelize.File) error {
	sheet := "Transactions"

	// Headers
	headers := []string{"Date", "Description", "Type", "Amount", "Balance"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Data
	for i, txn := range p.Transactions {
		row := i + 2

		// Date
		cell, _ := excelize.CoordinatesToCellName(1, row)
		f.SetCellValue(sheet, cell, txn.Date.Format("2006-01-02"))

		// Description
		cell, _ = excelize.CoordinatesToCellName(2, row)
		f.SetCellValue(sheet, cell, txn.Description)

		// Type
		cell, _ = excelize.CoordinatesToCellName(3, row)
		f.SetCellValue(sheet, cell, txn.Type)

		// Amount
		cell, _ = excelize.CoordinatesToCellName(4, row)
		if txn.Amount > 0 {
			f.SetCellValue(sheet, cell, txn.Amount)
		}

		// Balance
		cell, _ = excelize.CoordinatesToCellName(5, row)
		if txn.Balance > 0 {
			f.SetCellValue(sheet, cell, txn.Balance)
		}
	}

	// Apply header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	f.SetCellStyle(sheet, "A1", "E1", headerStyle)

	// Number format for amounts
	numStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 4, // #,##0.00
	})
	if len(p.Transactions) > 0 {
		lastRow := len(p.Transactions) + 1
		f.SetCellStyle(sheet, "D2", fmt.Sprintf("D%d", lastRow), numStyle)
		f.SetCellStyle(sheet, "E2", fmt.Sprintf("E%d", lastRow), numStyle)
	}

	// Set column widths
	f.SetColWidth(sheet, "A", "A", 12)  // Date
	f.SetColWidth(sheet, "B", "B", 50)  // Description
	f.SetColWidth(sheet, "C", "C", 10)  // Type
	f.SetColWidth(sheet, "D", "D", 15)  // Amount
	f.SetColWidth(sheet, "E", "E", 15)  // Balance

	// Freeze header row
	f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	// Add autofilter
	if len(p.Transactions) > 0 {
		lastRow := len(p.Transactions) + 1
		f.AutoFilter(sheet, fmt.Sprintf("A1:E%d", lastRow), []excelize.AutoFilterOptions{})
	}

	return nil
}

// createSummarySheet creates the Summary sheet
func (p *BCAParser) createSummarySheet(f *excelize.File) error {
	sheet := "Summary"

	// Headers
	headers := []string{
		"Opening Balance",
		"Total Credits",
		"Credit Count",
		"Total Debits",
		"Debit Count",
		"Closing Balance",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Data
	data := []interface{}{
		p.Summary.OpeningBalance,
		p.Summary.TotalCredits,
		p.Summary.CreditCount,
		p.Summary.TotalDebits,
		p.Summary.DebitCount,
		p.Summary.ClosingBalance,
	}
	for i, value := range data {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		f.SetCellValue(sheet, cell, value)
	}

	// Apply header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#70AD47"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	for i := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Number format for amounts
	numStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 4, // #,##0.00
	})
	// Apply to all except count columns (3rd and 5th)
	f.SetCellStyle(sheet, "A2", "A2", numStyle)
	f.SetCellStyle(sheet, "B2", "B2", numStyle)
	f.SetCellStyle(sheet, "D2", "D2", numStyle)
	f.SetCellStyle(sheet, "F2", "F2", numStyle)

	// Set column widths
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, 18)
	}

	return nil
}