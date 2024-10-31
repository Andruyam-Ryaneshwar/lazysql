package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"lazysql/db"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v4"
)

type State int

const (
	StateLoading State = iota
	StateSelectUser
	StateSelectDatabase
	StateEnterPassword
	StateConnecting
	StateListTables
	StateCreateTableName
	StateCreateTableSchema
	StateViewTable
	StateAddRow
	StateError
)

type Model struct {
	state         State
	spinner       spinner.Model
	userList      list.Model
	databaseList  list.Model
	passwordInput textinput.Model
	tableList     list.Model
	conn          *pgx.Conn
	connErr       error
	selectedUser  string
	selectedDB    string
	userPassword  string
	dbConn        *pgx.Conn
	tables        []string
	err           error

	// Fields for table creation
	tableNameInput   textinput.Model
	tableSchemaInput textinput.Model
	tableName        string
	tableSchema      string

	// Fields for viewing table contents
	selectedTable     string
	tableData         []map[string]interface{}
	dataTable         table.Model
	tableColumns      []string
	addRowInputs      []textinput.Model
	currentInputIndex int

	windowSize tea.WindowSizeMsg
}

type postgresFoundMsg struct{}
type postgresNotFoundMsg struct{}
type usersMsg struct{ users []string }
type databasesMsg struct{ databases []string }
type connectedMsg struct{ conn *pgx.Conn }
type tablesMsg struct{ tables []string }
type tableCreatedMsg struct{}
type tableDataMsg struct{ data []map[string]interface{} }
type tableColumnsMsg struct{ columns []string }
type rowInsertedMsg struct{}
type errMsg struct{ err error }

type myListItem struct {
	title, desc string
}

func (i myListItem) Title() string       { return i.title }
func (i myListItem) Description() string { return i.desc }
func (i myListItem) FilterValue() string { return i.title }

var (
	spinnerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	tableStyle    = table.DefaultStyles()
)

func convertToListItems(items []string) []list.Item {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = myListItem{title: item}
	}
	return listItems
}

// Custom delegate for styling list items
type customDelegate struct{}

func (d customDelegate) Height() int                               { return 1 }
func (d customDelegate) Spacing() int                              { return 0 }
func (d customDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d customDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(myListItem)
	if !ok {
		return
	}

	var (
		titleStyle    = normalStyle.PaddingLeft(2)
		selectedTitle = selectedStyle.PaddingLeft(2)
		cursor        = " "
		title         = i.Title()
		renderStyle   = titleStyle
	)

	if index == m.Index() {
		cursor = ">"
		renderStyle = selectedTitle
	}

	fmt.Fprintf(w, "%s %s\n", cursor, renderStyle.Render(title))
}

func initializeModel() *Model {
	s := spinner.New()
	s.Style = spinnerStyle

	// Custom list styles to remove unwanted lines
	listStyles := list.DefaultStyles()
	listStyles.Title = lipgloss.NewStyle().Bold(true).PaddingLeft(2)
	listStyles.PaginationStyle = lipgloss.NewStyle()
	listStyles.HelpStyle = lipgloss.NewStyle()
	listStyles.FilterCursor = lipgloss.NewStyle()
	listStyles.FilterPrompt = lipgloss.NewStyle()
	listStyles.NoItems = lipgloss.NewStyle().PaddingLeft(2)

	delegate := customDelegate{}

	userList := list.New([]list.Item{}, delegate, 0, 0)
	userList.Title = "Select User"
	userList.SetShowStatusBar(false)
	userList.SetFilteringEnabled(false)
	userList.Styles = listStyles

	databaseList := list.New([]list.Item{}, delegate, 0, 0)
	databaseList.Title = "Select Database"
	databaseList.SetShowStatusBar(false)
	databaseList.SetFilteringEnabled(false)
	databaseList.Styles = listStyles

	passwordInput := textinput.New()
	passwordInput.Placeholder = "Enter password"
	passwordInput.EchoMode = textinput.EchoPassword
	// Remove unnecessary focus
	// passwordInput.Focus()

	tableList := list.New([]list.Item{}, delegate, 0, 0)
	tableList.Title = "Tables"
	tableList.SetShowStatusBar(false)
	tableList.SetFilteringEnabled(false)
	tableList.Styles = listStyles

	dataTable := table.New()
	dataTable.SetStyles(tableStyle)

	return &Model{
		state:         StateLoading,
		spinner:       s,
		userList:      userList,
		databaseList:  databaseList,
		passwordInput: passwordInput,
		tableList:     tableList,
		dataTable:     dataTable,
	}
}

func checkDbInstalled() tea.Cmd {
	return func() tea.Msg {
		if db.IsPostgresInstalled() {
			return postgresFoundMsg{}
		}
		return postgresNotFoundMsg{}
	}
}

func fetchUsers(conn *pgx.Conn) tea.Cmd {
	return func() tea.Msg {
		users, err := db.GetUsers(conn)
		if err != nil {
			return errMsg{err: err}
		}
		return usersMsg{users: users}
	}
}

func fetchDatabases(conn *pgx.Conn) tea.Cmd {
	return func() tea.Msg {
		databases, err := db.GetDatabases(conn)
		if err != nil {
			return errMsg{err: err}
		}
		return databasesMsg{databases: databases}
	}
}

func connectAsUser(username, password, database string) tea.Cmd {
	return func() tea.Msg {
		conn, err := db.ConnectAsUser(username, password, database)
		if err != nil {
			return errMsg{err: err}
		}
		return connectedMsg{conn: conn}
	}
}

func fetchTables(conn *pgx.Conn) tea.Cmd {
	return func() tea.Msg {
		tables, err := db.GetTables(conn)
		if err != nil {
			return errMsg{err: err}
		}
		return tablesMsg{tables: tables}
	}
}

func createTable(conn *pgx.Conn, tableName, schema string) tea.Cmd {
	return func() tea.Msg {
		err := db.CreateTable(conn, tableName, schema)
		if err != nil {
			return errMsg{err: err}
		}
		return tableCreatedMsg{}
	}
}

func fetchTableData(conn *pgx.Conn, tableName string) tea.Cmd {
	return func() tea.Msg {
		data, err := db.GetTableData(conn, tableName)
		if err != nil {
			return errMsg{err: err}
		}
		return tableDataMsg{data: data}
	}
}

func fetchTableColumns(conn *pgx.Conn, tableName string) tea.Cmd {
	return func() tea.Msg {
		columns, err := db.GetTableColumns(conn, tableName)
		if err != nil {
			return errMsg{err: err}
		}
		return tableColumnsMsg{columns: columns}
	}
}

func insertRow(conn *pgx.Conn, tableName string, values map[string]interface{}) tea.Cmd {
	return func() tea.Msg {
		err := db.InsertRow(conn, tableName, values)
		if err != nil {
			return errMsg{err: err}
		}
		return rowInsertedMsg{}
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		checkDbInstalled(),
	)
}

func (m *Model) adjustListSizes() {
	listWidth := m.windowSize.Width - 4
	listHeight := m.windowSize.Height - 10
	m.userList.SetSize(listWidth, listHeight)
	m.databaseList.SetSize(listWidth, listHeight)
	m.tableList.SetSize(listWidth, listHeight)
	m.dataTable.SetWidth(listWidth)
	m.dataTable.SetHeight(listHeight)
}

func handleGlobalKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Handle window size messages
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowSize = msg
		m.adjustListSizes()
	}

	// Handle global key presses
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd := handleGlobalKeys(keyMsg); cmd != nil {
			return m, cmd
		}
	}

	switch m.state {
	case StateLoading:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case postgresFoundMsg:
			m.state = StateSelectUser
			conn, err := db.ConnectAsUser("postgres", "postgres", "postgres")
			if err != nil {
				m.err = err
				m.state = StateError
				return m, nil
			}
			m.conn = conn
			cmds = append(cmds, fetchUsers(conn))
		case postgresNotFoundMsg:
			m.err = fmt.Errorf("Postgres is not installed or not running!")
			m.state = StateError
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateSelectUser:
		m.userList, cmd = m.userList.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case usersMsg:
			m.userList.SetItems(convertToListItems(msg.users))
			cmds = append(cmds, fetchDatabases(m.conn))
		case databasesMsg:
			m.databaseList.SetItems(convertToListItems(msg.databases))
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				selectedItem := m.userList.SelectedItem()
				if selectedItem != nil {
					m.selectedUser = selectedItem.(myListItem).title
					m.state = StateSelectDatabase
				}
			}
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateSelectDatabase:
		m.databaseList, cmd = m.databaseList.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				selectedItem := m.databaseList.SelectedItem()
				if selectedItem != nil {
					m.selectedDB = selectedItem.(myListItem).title
					m.state = StateEnterPassword
					m.passwordInput.Focus()
				}
			}
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateEnterPassword:
		m.passwordInput, cmd = m.passwordInput.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.userPassword = m.passwordInput.Value()
				m.passwordInput.Reset()
				m.state = StateConnecting
				cmds = append(cmds, connectAsUser(m.selectedUser, m.userPassword, m.selectedDB))
			}
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateConnecting:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case connectedMsg:
			m.dbConn = msg.conn
			m.state = StateListTables
			cmds = append(cmds, fetchTables(m.dbConn))
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateListTables:
		m.tableList, cmd = m.tableList.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tablesMsg:
			m.tableList.SetItems(convertToListItems(msg.tables))
		case tableCreatedMsg:
			cmds = append(cmds, fetchTables(m.dbConn))
			m.state = StateListTables
		case tea.KeyMsg:
			switch msg.String() {
			case "n":
				m.initTableCreationInputs()
				m.state = StateCreateTableName
			case "enter":
				selectedItem := m.tableList.SelectedItem()
				if selectedItem != nil {
					m.selectedTable = selectedItem.(myListItem).title
					cmds = append(cmds, fetchTableData(m.dbConn, m.selectedTable))
					m.state = StateViewTable
				}
			}
		case tableDataMsg:
			m.tableData = msg.data
			m.initDataTable()
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateCreateTableName:
		m.tableNameInput, cmd = m.tableNameInput.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.tableName = m.tableNameInput.Value()
				if m.tableName == "" {
					m.err = fmt.Errorf("Table name cannot be empty")
				} else {
					m.initTableSchemaInput()
					m.state = StateCreateTableSchema
				}
			case "esc":
				m.state = StateListTables
			}
		case errMsg:
			m.err = msg.err
		}
	case StateCreateTableSchema:
		m.tableSchemaInput, cmd = m.tableSchemaInput.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.tableSchema = m.tableSchemaInput.Value()
				if m.tableSchema == "" {
					m.err = fmt.Errorf("Table schema cannot be empty")
				} else {
					cmds = append(cmds, createTable(m.dbConn, m.tableName, m.tableSchema))
					m.state = StateListTables // Corrected state transition
				}
			case "esc":
				m.state = StateListTables
			}
		case errMsg:
			m.err = msg.err
		case tableCreatedMsg:
			cmds = append(cmds, fetchTables(m.dbConn))
			m.state = StateListTables
			m.err = nil // Clear any previous errors
		}
	case StateViewTable:
		m.dataTable, cmd = m.dataTable.Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.state = StateListTables
			case "a":
				cmds = append(cmds, fetchTableColumns(m.dbConn, m.selectedTable))
				// Transition to StateAddRow happens after columns are fetched
			}
		case tableColumnsMsg:
			m.tableColumns = msg.columns
			m.initAddRowInputs()
			m.state = StateAddRow
		case rowInsertedMsg:
			cmds = append(cmds, fetchTableData(m.dbConn, m.selectedTable))
			m.state = StateViewTable
		case tableDataMsg:
			m.tableData = msg.data
			m.initDataTable()
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateAddRow:
		// Update the current focused input
		m.addRowInputs[m.currentInputIndex], cmd = m.addRowInputs[m.currentInputIndex].Update(msg)
		cmds = append(cmds, cmd)

		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				if m.currentInputIndex < len(m.addRowInputs)-1 {
					// Move to the next input
					m.addRowInputs[m.currentInputIndex].Blur()
					m.currentInputIndex++
					m.addRowInputs[m.currentInputIndex].Focus()
				} else {
					// All inputs are filled, insert the row
					values := make(map[string]interface{})
					for i, col := range m.tableColumns {
						values[col] = m.addRowInputs[i].Value()
					}
					cmds = append(cmds, insertRow(m.dbConn, m.selectedTable, values))
					// Reset inputs for next time
					m.addRowInputs = nil
					m.currentInputIndex = 0
					// Corrected state transition
					m.state = StateViewTable
				}
			case "esc":
				m.state = StateViewTable
			case "tab", "shift+tab":
				// Handle tab navigation
				m.addRowInputs[m.currentInputIndex].Blur()
				if msg.String() == "tab" {
					m.currentInputIndex = (m.currentInputIndex + 1) % len(m.addRowInputs)
				} else {
					m.currentInputIndex = (m.currentInputIndex - 1 + len(m.addRowInputs)) % len(m.addRowInputs)
				}
				m.addRowInputs[m.currentInputIndex].Focus()
			}
		case errMsg:
			m.err = msg.err
			m.state = StateError
		case rowInsertedMsg:
			cmds = append(cmds, fetchTableData(m.dbConn, m.selectedTable))
			m.state = StateViewTable
		case tableDataMsg:
			m.tableData = msg.data
			m.initDataTable()
		}
	case StateError:
		switch msg.(type) {
		case tea.KeyMsg:
			m.err = nil
			// Return to the previous state or a safe state
			m.state = StateListTables
		default:
			return m, nil
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) initTableCreationInputs() {
	m.tableNameInput = textinput.New()
	m.tableNameInput.Placeholder = "Enter table name"
	m.tableNameInput.Prompt = "Table Name: "
	m.tableNameInput.Focus()
}

func (m *Model) initTableSchemaInput() {
	m.tableSchemaInput = textinput.New()
	m.tableSchemaInput.Placeholder = "id SERIAL PRIMARY KEY, name TEXT"
	m.tableSchemaInput.Prompt = "Table Schema: "
	m.tableSchemaInput.Focus()
}

func (m *Model) initDataTable() {
	var columns []table.Column
	var rows []table.Row

	if len(m.tableData) == 0 {
		// Fetch columns
		columnsList, err := db.GetTableColumns(m.dbConn, m.selectedTable)
		if err != nil {
			m.err = err
			m.state = StateError
			return
		}
		for _, col := range columnsList {
			columns = append(columns, table.Column{Title: col, Width: 20})
		}
	} else {
		// Get columns from data
		for col := range m.tableData[0] {
			columns = append(columns, table.Column{Title: col, Width: 20})
		}

		// Create rows
		for _, rowData := range m.tableData {
			row := table.Row{}
			for _, col := range columns {
				value := fmt.Sprintf("%v", rowData[col.Title])
				row = append(row, value)
			}
			rows = append(rows, row)
		}
	}

	m.dataTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(m.windowSize.Height-10),
		table.WithWidth(m.windowSize.Width-4),
	)
	m.dataTable.SetStyles(tableStyle)
}

func (m *Model) initAddRowInputs() {
	m.addRowInputs = make([]textinput.Model, len(m.tableColumns))
	for i, col := range m.tableColumns {
		input := textinput.New()
		input.Placeholder = fmt.Sprintf("Enter %s", col)
		input.Prompt = fmt.Sprintf("%s: ", col)
		input.CharLimit = 50
		if i == 0 {
			input.Focus()
		}
		m.addRowInputs[i] = input
	}
	m.currentInputIndex = 0
}

func (m *Model) View() string {
	var header string
	// Corrected header construction
	switch m.state {
	case StateSelectUser:
		header = "Select a User"
	case StateSelectDatabase:
		header = fmt.Sprintf("Selected User: %s", selectedStyle.Render(m.selectedUser))
	default:
		if m.selectedUser != "" && m.selectedDB != "" {
			header = fmt.Sprintf("Selected User: %s | Selected Database: %s", selectedStyle.Render(m.selectedUser), selectedStyle.Render(m.selectedDB))
		} else if m.selectedUser != "" {
			header = fmt.Sprintf("Selected User: %s", selectedStyle.Render(m.selectedUser))
		}
	}

	errorMsg := ""
	if m.err != nil {
		errorMsg = fmt.Sprintf("\n\nError: %v", m.err)
	}

	switch m.state {
	case StateLoading:
		return fmt.Sprintf("\n  %s Checking PostgreSQL installation...", m.spinner.View())
	case StateSelectUser:
		return fmt.Sprintf("\n%s\n\n%s", header, m.userList.View())
	case StateSelectDatabase:
		return fmt.Sprintf("\n%s\n\n%s", header, m.databaseList.View())
	case StateEnterPassword:
		return fmt.Sprintf("\n%s\n\nEnter password for user '%s' on database '%s':\n\n%s%s", header, m.selectedUser, m.selectedDB, m.passwordInput.View(), errorMsg)
	case StateConnecting:
		return fmt.Sprintf("\n%s\n\n  %s Connecting to database...", header, m.spinner.View())
	case StateListTables:
		instructions := "\n\nPress 'n' to create a new table, 'q' to quit."
		return fmt.Sprintf("\n%s\n\n%s%s%s", header, m.tableList.View(), instructions, errorMsg)
	case StateCreateTableName:
		return fmt.Sprintf(
			"\n%s\n\nCreate New Table\n\n%s%s\n\nPress Enter to continue, Esc to cancel.",
			header,
			m.tableNameInput.View(),
			errorMsg,
		)
	case StateCreateTableSchema:
		return fmt.Sprintf(
			"\n%s\n\nCreate New Table\n\n%s%s\n\nPress Enter to create table, Esc to cancel.",
			header,
			m.tableSchemaInput.View(),
			errorMsg,
		)
	case StateViewTable:
		noDataMsg := ""
		if len(m.tableData) == 0 {
			noDataMsg = "\n\nNo data in this table."
		}
		instructions := "\n\nPress 'a' to add a new row, 'esc' to go back."
		return fmt.Sprintf("\n%s\n\nViewing Table: %s%s%s\n\n%s", header, selectedStyle.Render(m.selectedTable), noDataMsg, instructions, m.dataTable.View())
	case StateAddRow:
		var inputsView strings.Builder
		for i, input := range m.addRowInputs {
			if i == m.currentInputIndex {
				inputsView.WriteString(selectedStyle.Render(input.View()))
			} else {
				inputsView.WriteString(input.View())
			}
			inputsView.WriteString("\n")
		}
		instructions := "\n\nPress Enter to proceed, Tab to navigate, Esc to cancel."
		return fmt.Sprintf(
			"\n%s\n\nAdd New Row to Table: %s\n\n%s%s%s",
			header,
			selectedStyle.Render(m.selectedTable),
			inputsView.String(),
			instructions,
			errorMsg,
		)
	case StateError:
		return fmt.Sprintf("\nAn error occurred: %v\n\nPress any key to continue.", m.err)
	default:
		return "\nUnknown state"
	}
}

func main() {
	model := initializeModel()

	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}

	if model.conn != nil {
		if err := model.conn.Close(context.Background()); err != nil {
			log.Printf("Error closing connection: %v", err)
		}
	}

	if model.dbConn != nil {
		if err := model.dbConn.Close(context.Background()); err != nil {
			log.Printf("Error closing database connection: %v", err)
		}
	}
}
