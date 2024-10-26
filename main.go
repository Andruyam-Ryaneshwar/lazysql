package main

import (
	"context"
	"fmt"
	"log"

	"lazysql/db"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
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
}

type postgresFoundMsg struct{}
type postgresNotFoundMsg struct{}
type usersMsg struct{ users []string }
type databasesMsg struct{ databases []string }
type connectedMsg struct{ conn *pgx.Conn }
type tablesMsg struct{ tables []string }
type errMsg struct{ err error }

type listItem struct {
	title, desc string
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

func convertToListItems(items []string) []list.Item {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = listItem{title: item}
	}
	return listItems
}

func initializeModel() Model {
	s := spinner.New()
	s.Style = spinnerStyle

	userList := list.New([]list.Item{}, list.NewDefaultDelegate(), 50, 10)
	userList.Title = "Select User"

	databaseList := list.New([]list.Item{}, list.NewDefaultDelegate(), 50, 10)
	databaseList.Title = "Select Database"

	passwordInput := textinput.New()
	passwordInput.Placeholder = "Enter password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.Focus()

	tableList := list.New([]list.Item{}, list.NewDefaultDelegate(), 50, 10)
	tableList.Title = "Tables"

	return Model{
		state:         StateLoading,
		spinner:       s,
		userList:      userList,
		databaseList:  databaseList,
		passwordInput: passwordInput,
		tableList:     tableList,
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		checkDbInstalled(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

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
					m.selectedUser = selectedItem.(listItem).title
					m.state = StateSelectDatabase
				}
			case "ctrl+c", "q":
				return m, tea.Quit
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
					m.selectedDB = selectedItem.(listItem).title
					m.state = StateEnterPassword
					m.passwordInput.Focus()
				}
			case "ctrl+c", "q":
				return m, tea.Quit
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
			case "ctrl+c", "q":
				return m, tea.Quit
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
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			}
		case errMsg:
			m.err = msg.err
			m.state = StateError
		}
	case StateError:
		switch msg.(type) {
		case tea.KeyMsg:
			return m, tea.Quit
		default:
			return m, nil
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	switch m.state {
	case StateLoading:
		return fmt.Sprintf("\n  %s Checking PostgreSQL installation...", m.spinner.View())
	case StateSelectUser:
		return "\n" + m.userList.View()
	case StateSelectDatabase:
		return "\n" + m.databaseList.View()
	case StateEnterPassword:
		return fmt.Sprintf("\nEnter password for user '%s' on database '%s':\n\n%s", m.selectedUser, m.selectedDB, m.passwordInput.View())
	case StateConnecting:
		return fmt.Sprintf("\n  %s Connecting to database...", m.spinner.View())
	case StateListTables:
		return "\n" + m.tableList.View()
	case StateError:
		return fmt.Sprintf("\nAn error occurred: %v\n\nPress any key to exit.", m.err)
	default:
		return "\nUnknown state"
	}
}

func main() {
	model := initializeModel()

	p := tea.NewProgram(model)

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
