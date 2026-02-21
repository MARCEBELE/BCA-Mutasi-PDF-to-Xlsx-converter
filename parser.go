package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)


// Transaction represents a single bank transaction
type Transaction struct {
	Date        time.Time
	Description string
	Type        string // CR, DB, OPENING
	Amount      float64
	Balance     float64
}

// AccountInfo holds account details
type AccountInfo struct {
	AccountNumber string
	AccountHolder string
	Period        string
	Currency      string
}

// Summary holds transaction summary
type Summary struct {
	OpeningBalance float64
	TotalCredits   float64
	CreditCount    int
	TotalDebits    float64
	DebitCount     int
	ClosingBalance float64
}

// BCAParser handles parsing of BCA statements
type BCAParser struct {
	Filename     string
	AccountInfo  AccountInfo
	Transactions []Transaction
	Summary      Summary
}

// NewBCAParser creates a new parser instance
func NewBCAParser(filename string) *BCAParser {
	return &BCAParser{
		Filename:     filename,
		Transactions: make([]Transaction, 0),
	}
}

// Parse reads and parses the TXT file
func (p *BCAParser) Parse() error {
	file, err := os.Open(p.Filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read entire file
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	content := strings.Join(lines, "\n")

	// Extract account info
	p.extractAccountInfo(content, lines)

	// Extract transactions
	p.extractTransactions(lines)

	// Extract summary
	p.extractSummary(content)

	return nil
}

// extractAccountInfo extracts account information from content
func (p *BCAParser) extractAccountInfo(content string, lines []string) {
	// Account number
	re := regexp.MustCompile(`NO\.\s*REKENING\s*:\s*(\d+)`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.AccountInfo.AccountNumber = match[1]
	}

	// Period
	re = regexp.MustCompile(`PERIODE\s*:\s*([A-Z]+\s+\d+)`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.AccountInfo.Period = match[1]
	}

	// Account holder: in PDF-derived TXT files the name sits on the same line
	// as "NO. REKENING", e.g. "RAIHAN RIZKI MAULANA AMSAD NO. REKENING : 59407…"
	// Extract everything before "NO." on that line as the holder name.
	// Fall back to lines[2] for BCA's own TXT exports (where the name is on
	// the third line of the file).
	re = regexp.MustCompile(`(?m)^(.+?)\s+NO\.\s*REKENING\s*:`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.AccountInfo.AccountHolder = strings.TrimSpace(match[1])
	} else if len(lines) > 2 {
		p.AccountInfo.AccountHolder = strings.TrimSpace(lines[2])
	}

	// Currency
	re = regexp.MustCompile(`MATA\s+UANG\s*:\s*(\w+)`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.AccountInfo.Currency = match[1]
	}
}

// spbuPumpCode matches the pump/nozzle code that BCA embeds immediately after "KARTU DEBIT SPBU ".
// These codes look like "34.15147", "34.15151-HO", "31.117.02", "34-15129" etc.
// They must be stripped before amount extraction because multi-dot codes such as "31.117.02"
// contain a segment ("117.02") that is indistinguishable from a real currency amount. (Bug B-1b)
var spbuPumpCode = regexp.MustCompile(`(?i)(KARTU DEBIT SPBU\s+)([\d.,\-]+)`)

// embeddedTxnDate detects a DD/MM date token that appears in the MIDDLE of a line
// immediately after a non-space, non-colon character — this happens when the PDF
// renderer merges two adjacent lines without a newline, e.g.:
//   "MCD RUKO SUDIRMAN19/01 TRSF E-BANKING CR 1901/ATSCY/WS95051 3,525,000.00"
// A date that is legitimately part of a description always follows a space or colon,
// e.g. "TANGGAL :07/03" or "TGL: 18/07", so those are not matched.
var embeddedTxnDate = regexp.MustCompile(`(?:[^\s:])(\d{2}/\d{2}\s)`)

// extractTransactions parses all transactions using a state machine.
//
// BCA TXT files have a fixed structure per page:
//   [header block] → "TANGGAL KETERANGAN CBG MUTASI SALDO" → [transaction rows]
//
// The header block contains the branch name, product name, account holder name,
// address lines, account number, period, currency, disclaimer, etc.
// NONE of that is hardcoded here — we simply skip everything until we see the
// column-header sentinel line, then start collecting transactions.
//
// This makes the parser universal for any BCA account holder, branch, or
// product variant without any name/address lists.
// inlinePageBreak matches the entire page-break header blob that 2026+ BCA PDFs
// render as a single line instead of separate lines.  The blob starts with
// "Bersambung ke halaman berikut" and ends at (and includes) the column-header
// sentinel "TANGGAL KETERANGAN CBG MUTASI SALDO".
// After stripping, what remains on the line is the clean transaction text.
// inlinePageBreak matches the page-break header blob that 2026+ BCA PDFs
// render as one long line appended to the last transaction on the page.
// The blob always ends with the BCA disclaimer closing bullet character •.
// We match from "Bersambung ke halaman berikut" to the final • at end-of-line.
var inlinePageBreak = regexp.MustCompile(
	`(?i)Bersambung ke halaman berikut\b.*?[\x{2022}\xE2\x80\xA2]\s*$`,
)

func (p *BCAParser) extractTransactions(lines []string) {
	const sentinel = "TANGGAL KETERANGAN CBG MUTASI SALDO"

	var buffer []string
	year := p.getYearFromPeriod()
	lastMonth := 0
	inTransactions := false // flip to true after we pass the sentinel
	inPageHeader  := false  // true while skipping inter-page header block

	datePattern  := regexp.MustCompile(`^\d{2}/\d{2}\s`)
	monthCapture := regexp.MustCompile(`^\d{2}/(\d{2})`)

	// flushBuffer processes the buffered transaction and handles the
	// December→January year rollover: if the new transaction's month is
	// January (01) and the previous was December (12), the working year is
	// incremented before the date is parsed.
	flushBuffer := func() {
		if len(buffer) == 0 {
			return
		}
		if m := monthCapture.FindStringSubmatch(buffer[0]); len(m) > 1 {
			month, _ := strconv.Atoi(m[1])
			if lastMonth == 12 && month == 1 {
				year++
			}
			if month > 0 {
				lastMonth = month
			}
		}
		p.processTransactionBuffer(buffer, year)
		buffer = nil
	}

	for _, rawLine := range lines {
		// ── Strip inline page-break header (2026+ PDF format) ────────────
		// In newer BCA PDFs pdfplumber sometimes collapses the entire next-page
		// header onto the tail of the last transaction line as one long string.
		// Remove the blob here so both pdf→txt conversion AND direct TXT parsing
		// handle it correctly.
		rawLine = inlinePageBreak.ReplaceAllString(rawLine, "")

		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		// ── Page-break header lines ─────────────────────────────────────
		// "Bersambung ke halaman berikut" and everything after it until the
		// sentinel are page-header lines, not transaction content.
		// Set a flag so we drop continuation lines during the header block.
		if strings.Contains(line, "Bersambung ke halaman berikut") {
			inPageHeader = true
			continue
		}
		if inPageHeader {
			// Still inside the inter-page header block — skip all lines until
			// we hit the sentinel below.
			if !strings.Contains(line, sentinel) {
				continue
			}
			// Fall through to sentinel handling below.
		}

		// ── Wait for the column-header sentinel ──────────────────────────
		// Every BCA page starts with a header block and ends with this line.
		// We skip everything before it (name, address, branch, disclaimer…).
		if strings.Contains(line, sentinel) {
			inPageHeader = false
			flushBuffer()
			inTransactions = true
			continue
		}

		if !inTransactions {
			continue // still in page header — skip
		}

		// ── Inside the transaction section ───────────────────────────────

		// Skip the summary footer lines (end of file, not transactions)
		if p.isSummaryLine(line) {
			flushBuffer()
			// After the summary block the file ends; reset so any trailing
			// page-continuation header is skipped too
			inTransactions = false
			continue
		}

		// Skip bare page numbers like "1 /" "2 /" "7 / 7"
		if pageNumberPattern.MatchString(line) {
			continue
		}

		// New transaction line: starts with DD/MM followed by a space
		if datePattern.MatchString(line) {
			flushBuffer()
			buffer = append(buffer, line)
			continue
		}

		// Continuation line (description fragment, beneficiary name, etc.)
		if len(buffer) > 0 {
			// Check for an embedded transaction date mid-line (PDF merge artefact).
			// e.g. "MCD RUKO SUDIRMAN19/01 TRSF E-BANKING CR ..."
			if loc := embeddedTxnDate.FindStringIndex(line); loc != nil {
				splitAt := loc[0] + 1
				prefix := strings.TrimSpace(line[:splitAt])
				newStart := strings.TrimSpace(line[splitAt:])
				if prefix != "" {
					buffer = append(buffer, prefix)
				}
				flushBuffer()
				buffer = []string{newStart}
			} else {
				buffer = append(buffer, line)
			}
		}
		// Lines that arrive before the first date line (between sentinel and
		// first transaction) are ignored — this covers the "5" page-count
		// digit that 2025 files print right after the sentinel.
	}

	flushBuffer()
}

// pageNumberPattern matches bare page markers: "1 /", "3 / 7", "7"  etc.
// We require a slash so bare small integers (branch codes) are not caught.
var pageNumberPattern = regexp.MustCompile(`^\d+\s*/\s*\d*$`)

// isSummaryLine returns true for the four footer lines that end the file.
// These are fixed BCA labels followed by a colon — safe to match precisely.
func (p *BCAParser) isSummaryLine(line string) bool {
	return strings.HasPrefix(line, "SALDO AWAL :") ||
		strings.HasPrefix(line, "MUTASI CR :") ||
		strings.HasPrefix(line, "MUTASI DB :") ||
		strings.HasPrefix(line, "SALDO AKHIR :")
}

// isSkipLine is retained for completeness but is no longer called by
// extractTransactions (the state machine makes per-line decisions itself).
// It can still be used externally if needed.
func (p *BCAParser) isSkipLine(line string) bool {
	return strings.Contains(line, "TANGGAL KETERANGAN CBG MUTASI SALDO") ||
		p.isSummaryLine(line) ||
		pageNumberPattern.MatchString(line)
}

// echoLinePattern matches bare reference-number echo lines BCA inserts after some transactions
// e.g. "331000.00" or "7935800.00" — no commas, exactly 2 decimal places, nothing else.
var echoLinePattern = regexp.MustCompile(`^\d+\.\d{2}$`)

// tanggalEchoPattern matches lines like "TANGGAL :07/03 110000.00" that combine a date
// back-reference with a comma-free echo amount.  The whole line is metadata and must be
// stripped so the echo number doesn't shift the Amount/Balance index. (Bug B-2)
var tanggalEchoPattern = regexp.MustCompile(`^TANGGAL\s*:\d{2}/\d{2}\s+\d+\.\d{2}$`)

// isEchoLine returns true if the continuation line should be stripped before amount extraction.
func isEchoLine(line string) bool {
	return echoLinePattern.MatchString(line) || tanggalEchoPattern.MatchString(line)
}

// wordBoundary matches a standalone CR or DB token (preceded/followed by non-letter).
var crWordPattern = regexp.MustCompile(`(?:^|\W)CR(?:\W|$)`)
var dbWordPattern = regexp.MustCompile(`(?:^|\W)DB(?:\W|$)`)

// detectType determines whether a transaction is CR or DB.
//
// Rules (in priority order):
//  1. Explicit keywords in the primary date line (buffer[0]).
//  2. KR INTERCHANGE anywhere → CR  (Bug B-4: BCA uses this prefix for card refunds).
//  3. Word-boundary scan of fullText as a fallback for transactions where the amount
//     and type marker appear on a continuation line (e.g. KARTU DEBIT SPBU where
//     "38,357.00 DB" is on its own line).
//
// Checking buffer[0] before fullText prevents false CR hits from merchant names such
// as "MALAKA CRN" that happen to contain the substring " CR" (Bug B-3).
func detectType(dateLine, fullText string) string {
	// --- Primary: date/header line ---
	hasCRInLine := strings.Contains(dateLine, " CR") || strings.HasSuffix(dateLine, "CR")
	hasDBInLine := strings.Contains(dateLine, " DB") || strings.HasSuffix(dateLine, "DB")

	if hasCRInLine || strings.Contains(dateLine, "TRANSFER DR") {
		return "CR"
	}
	if hasDBInLine ||
		strings.Contains(dateLine, "TRANSFER KE") ||
		strings.Contains(dateLine, "TARIKAN ATM") ||
		strings.Contains(dateLine, "TRANSAKSI DEBIT") ||
		strings.Contains(dateLine, "DEBIT DOMESTIK") {
		return "DB"
	}

	// --- Secondary: KR INTERCHANGE anywhere (Bug B-4) ---
	if strings.Contains(fullText, "KR INTERCHANGE") {
		return "CR"
	}

	// --- Tertiary: word-boundary scan of fullText ---
	// Only used when the type marker is on a continuation line (e.g. SPBU where
	// amount+DB appear one line below the date line).
	// We give DB priority over CR here because merchant names occasionally contain
	// "CR" as part of abbreviations but rarely contain standalone "DB".
	if dbWordPattern.MatchString(fullText) {
		return "DB"
	}
	if crWordPattern.MatchString(fullText) {
		return "CR"
	}

	return ""
}

// processTransactionBuffer processes a buffered transaction
func (p *BCAParser) processTransactionBuffer(buffer []string, year int) {
	if len(buffer) == 0 {
		return
	}

	// Strip echo lines and QR zero-prefix lines before joining.
	// Echo lines: bare "331000.00" — BCA reference duplicates (Bug B-2).
	// QR zero-prefix: "00000.00MERCHANTNAME" — BCA internal code fused with merchant name.
	//   Strip the "00000.00" prefix so only the merchant name remains.
	// We keep buffer[0] (the date line) untouched; only continuation lines are filtered.
	filteredBuffer := []string{buffer[0]}
	qrZeroPrefix := regexp.MustCompile(`^0+\.00(.+)$`) // e.g. "00000.00TIX ID" → "TIX ID"
	for _, l := range buffer[1:] {
		if isEchoLine(l) {
			continue // drop echo line entirely
		}
		// Strip the QR zero-prefix, keep the merchant name
		if m := qrZeroPrefix.FindStringSubmatch(l); m != nil {
			l = strings.TrimSpace(m[1])
		}
		if l != "" {
			filteredBuffer = append(filteredBuffer, l)
		}
	}

	fullText := strings.Join(filteredBuffer, " ")

	// Strip SPBU pump/nozzle codes before amount extraction.
	// Pump codes like "31.117.02" contain segments (e.g. "117.02") that the amount
	// regex cannot distinguish from real currency amounts. (Bug B-1b)
	if strings.Contains(fullText, "KARTU DEBIT SPBU") {
		fullText = spbuPumpCode.ReplaceAllString(fullText, "${1}")
	}

	// Extract date
	datePattern := regexp.MustCompile(`^(\d{2}/\d{2})`)
	dateMatch := datePattern.FindStringSubmatch(buffer[0])
	if len(dateMatch) < 2 {
		return
	}

	txnDate := p.parseDate(dateMatch[1], year)
	if txnDate.IsZero() {
		return
	}

	txn := Transaction{
		Date:   txnDate,
		Type:   "",
		Amount: 0.0,
	}

	// Handle special cases
	if strings.Contains(fullText, "SALDO AWAL") {
		txn.Description = "SALDO AWAL"
		txn.Type = "OPENING"
		amounts := p.extractAmounts(fullText)
		if len(amounts) > 0 {
			// Store opening balance only in Balance; leave Amount = 0 so that
			// any SUM over the Amount column does not include the opening balance. (Bug B-5)
			txn.Balance = amounts[0]
		}
		p.Transactions = append(p.Transactions, txn)
		return
	}

	if strings.Contains(fullText, "BUNGA") && !strings.Contains(fullText, "PAJAK") {
		txn.Description = "BUNGA (Interest)"
		txn.Type = "CR"
		amounts := p.extractAmounts(fullText)
		if len(amounts) >= 1 {
			txn.Amount = amounts[0]
		}
		if len(amounts) >= 2 {
			txn.Balance = amounts[1]
		}
		p.Transactions = append(p.Transactions, txn)
		return
	}

	if strings.Contains(fullText, "PAJAK BUNGA") {
		txn.Description = "PAJAK BUNGA (Tax on Interest)"
		txn.Type = "DB"
		amounts := p.extractAmounts(fullText)
		if len(amounts) >= 1 {
			txn.Amount = amounts[0]
		}
		if len(amounts) >= 2 {
			txn.Balance = amounts[1]
		}
		p.Transactions = append(p.Transactions, txn)
		return
	}

	if strings.Contains(fullText, "BIAYA ADM") {
		txn.Description = "BIAYA ADM (Admin Fee)"
		txn.Type = "DB"
		amounts := p.extractAmounts(fullText)
		if len(amounts) >= 1 {
			txn.Amount = amounts[0]
		}
		if len(amounts) >= 2 {
			txn.Balance = amounts[1]
		}
		p.Transactions = append(p.Transactions, txn)
		return
	}

	// Determine transaction type.
	// Strategy: check buffer[0] (the date/header line) first.  This avoids false positives
	// from merchant names in continuation lines (e.g. "MALAKA CRN" containing " CR").
	// If buffer[0] is inconclusive, fall back to a word-boundary scan of the full text.
	txn.Type = detectType(buffer[0], fullText)

	// Build description
	description := fullText
	description = regexp.MustCompile(`^\d{2}/\d{2}\s*`).ReplaceAllString(description, "")
	
	// Extract and remove amounts
	amounts := p.extractAmounts(description)
	for _, amountStr := range p.extractAmountStrings(description) {
		description = strings.ReplaceAll(description, amountStr, "")
	}
	
	// Remove DB/CR type markers from description.
	// Strip only standalone word-boundary DB/CR tokens, not substrings of words.
	// e.g. "BI-FAST DB BIF TRANSFER KE" → "BI-FAST BIF TRANSFER KE"
	// but  "QRC014" stays intact (no space before DB/CR).
	dbcrMarker := regexp.MustCompile(`\b(DB|CR)\b`)
	description = dbcrMarker.ReplaceAllString(description, "")
	
	// Clean up spaces
	description = regexp.MustCompile(`\s+`).ReplaceAllString(description, " ")
	txn.Description = strings.TrimSpace(description)

	// Parse amounts
	if len(amounts) >= 2 {
		txn.Amount = amounts[len(amounts)-2]
		txn.Balance = amounts[len(amounts)-1]
	} else if len(amounts) == 1 {
		txn.Amount = amounts[0]
	}

	// Only add if we have a description
	if txn.Description != "" {
		p.Transactions = append(p.Transactions, txn)
	}
}

// parseDate converts DD/MM string to time.Time
func (p *BCAParser) parseDate(dateStr string, year int) time.Time {
	parts := strings.Split(dateStr, "/")
	if len(parts) != 2 {
		return time.Time{}
	}

	day, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])

	if err1 != nil || err2 != nil {
		return time.Time{}
	}

	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// extractAmounts extracts all amounts from text
func (p *BCAParser) extractAmounts(text string) []float64 {
	var amounts []float64
	// (?!\d) negative lookahead: reject matches where the 2-decimal digits are immediately
	// followed by more digits, e.g. "34.15" inside "SPBU 34.15147" (pump/merchant code).
	// Real BCA amounts are always terminated by a space, comma-separator, or end of string.
	pattern := regexp.MustCompile(`([\d,]+\.\d{2})(?:\D|$)`)
	matches := pattern.FindAllString(text, -1)

	for _, match := range matches {
		// The match may end with a trailing non-digit char (from the lookahead group).
		// Strip it before parsing the number.
		trimmed := regexp.MustCompile(`[\d,]+\.\d{2}`).FindString(match)
		if trimmed == "" {
			continue
		}
		amount := p.parseAmount(trimmed)
		if amount > 0 {
			amounts = append(amounts, amount)
		}
	}

	return amounts
}

// extractAmountStrings extracts amount strings (for removal from description)
func (p *BCAParser) extractAmountStrings(text string) []string {
	// Use the same safe pattern as extractAmounts so that merchant codes like
	// "SPBU 34.15147" are not mistakenly extracted and removed.
	outer := regexp.MustCompile(`([\d,]+\.\d{2})(?:\D|$)`)
	inner := regexp.MustCompile(`[\d,]+\.\d{2}`)
	var result []string
	for _, m := range outer.FindAllString(text, -1) {
		if s := inner.FindString(m); s != "" {
			result = append(result, s)
		}
	}
	return result
}

// parseAmount converts amount string to float64
func (p *BCAParser) parseAmount(amountStr string) float64 {
	cleaned := strings.ReplaceAll(amountStr, ",", "")
	amount, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0.0
	}
	return amount
}

// getYearFromPeriod extracts year from period string
func (p *BCAParser) getYearFromPeriod() int {
	pattern := regexp.MustCompile(`\d{4}`)
	if match := pattern.FindString(p.AccountInfo.Period); match != "" {
		year, err := strconv.Atoi(match)
		if err == nil {
			return year
		}
	}
	return time.Now().Year()
}

// extractSummary extracts summary information
func (p *BCAParser) extractSummary(content string) {
	// Opening balance
	re := regexp.MustCompile(`SALDO AWAL\s*:\s*([\d,]+\.\d{2})`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.Summary.OpeningBalance = p.parseAmount(match[1])
	}

	// Total credits
	re = regexp.MustCompile(`MUTASI CR\s*:\s*([\d,]+\.\d{2})\s+(\d+)`)
	if match := re.FindStringSubmatch(content); len(match) > 2 {
		p.Summary.TotalCredits = p.parseAmount(match[1])
		count, _ := strconv.Atoi(match[2])
		p.Summary.CreditCount = count
	}

	// Total debits
	re = regexp.MustCompile(`MUTASI DB\s*:\s*([\d,]+\.\d{2})\s+(\d+)`)
	if match := re.FindStringSubmatch(content); len(match) > 2 {
		p.Summary.TotalDebits = p.parseAmount(match[1])
		count, _ := strconv.Atoi(match[2])
		p.Summary.DebitCount = count
	}

	// Closing balance
	re = regexp.MustCompile(`SALDO AKHIR\s*:\s*([\d,]+\.\d{2})`)
	if match := re.FindStringSubmatch(content); len(match) > 1 {
		p.Summary.ClosingBalance = p.parseAmount(match[1])
	}
}

// PrintSummary displays parsing results
func (p *BCAParser) PrintSummary() {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("%sACCOUNT INFORMATION%s\n", ColorBlue, ColorReset)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("  Account Number : %s\n", p.AccountInfo.AccountNumber)
	fmt.Printf("  Account Holder : %s\n", p.AccountInfo.AccountHolder)
	fmt.Printf("  Period         : %s\n", p.AccountInfo.Period)
	fmt.Printf("  Currency       : %s\n", p.AccountInfo.Currency)

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("%sTRANSACTION SUMMARY%s\n", ColorBlue, ColorReset)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("  Total Transactions : %d\n", len(p.Transactions))
	fmt.Printf("  Opening Balance    : %s %s\n", p.AccountInfo.Currency, formatMoney(p.Summary.OpeningBalance))
	fmt.Printf("  Total Credits      : %s %s (%d transactions)\n", 
		p.AccountInfo.Currency, formatMoney(p.Summary.TotalCredits), p.Summary.CreditCount)
	fmt.Printf("  Total Debits       : %s %s (%d transactions)\n", 
		p.AccountInfo.Currency, formatMoney(p.Summary.TotalDebits), p.Summary.DebitCount)
	fmt.Printf("  Closing Balance    : %s %s\n", p.AccountInfo.Currency, formatMoney(p.Summary.ClosingBalance))
	fmt.Println(strings.Repeat("=", 70))
}

// formatMoney formats float as money string with thousand separators
func formatMoney(amount float64) string {
	s := fmt.Sprintf("%.2f", amount)
	parts := strings.Split(s, ".")
	intPart := parts[0]
	result := ""
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result + "." + parts[1]
}