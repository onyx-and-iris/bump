package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Table defines the interface for rendering a table of results in the bump CLI.
type Table interface {
	MustAddRow(columns ...string)
	Render() string
}

// styledTable implements the Table interface with predefined styling
type styledTable struct {
	table   *table.Table
	headers []string
}

// NewStyledTable creates a new table with the standard styling used in bump CLI.
// Accepts either 3 or 4 headers: ["File", "Current Version", "New Version"] or
// ["File", "Pattern", "Current Version", "New Version"]
func NewStyledTable(headers []string) (Table, error) {
	if len(headers) != 3 && len(headers) != 4 {
		return nil, fmt.Errorf("headers must contain exactly 3 or 4 elements")
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#C7D2FE"))).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)

			if row == table.HeaderRow {
				return style.Bold(true).Foreground(lipgloss.Color("#5B73E8"))
			}
			isEvenRow := row%2 == 0

			switch col {
			case 0:
				if isEvenRow {
					style = style.Foreground(lipgloss.Color("#6B7FD7"))
				} else {
					style = style.Foreground(lipgloss.Color("#8A9AE3"))
				}
			case 1:
				if len(headers) == 4 {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#9CA3F0")).Italic(true)
					} else {
						style = style.Foreground(lipgloss.Color("#B5C2F5")).Italic(true)
					}
				} else {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#5B73E8"))
					} else {
						style = style.Foreground(lipgloss.Color("#7A8DED"))
					}
					style = style.Align(lipgloss.Center)
				}
			case 2:
				if len(headers) == 4 {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#5B73E8"))
					} else {
						style = style.Foreground(lipgloss.Color("#7A8DED"))
					}
				} else {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#4F68E0"))
					} else {
						style = style.Foreground(lipgloss.Color("#6A7FE6"))
					}
				}
				style = style.Align(lipgloss.Center)
			case 3:
				if isEvenRow {
					style = style.Foreground(lipgloss.Color("#4F68E0"))
				} else {
					style = style.Foreground(lipgloss.Color("#6A7FE6"))
				}
				style = style.Align(lipgloss.Center)
			}

			return style
		})

	return &styledTable{
		table:   t,
		headers: headers,
	}, nil
}

// MustAddRow adds a row to the table and panics if the number of columns does not match the number of headers
func (st *styledTable) MustAddRow(columns ...string) {
	if len(columns) != len(st.headers) {
		panic("number of columns must match number of headers")
	}
	st.table.Row(columns...)
}

// Render returns the styled table as a string
func (st *styledTable) Render() string {
	return st.table.String()
}
