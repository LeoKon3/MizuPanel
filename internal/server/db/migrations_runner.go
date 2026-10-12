package db

import (
	"database/sql"
	"strings"
)

type migrationStepMode uint8

const (
	migrationStepExec migrationStepMode = iota
	migrationStepAddColumn
)

// migrationStep names the compatibility/domain phases without persisting a
// version marker. Every phase is intentionally replayed so databases created by
// older releases cannot skip a compatibility statement.
type migrationStep struct {
	name       string
	mode       migrationStepMode
	statements []string
}

func migrationSteps(dialect Dialect) []migrationStep {
	return []migrationStep{
		{name: "nodes.compatibility-columns", mode: migrationStepAddColumn, statements: nodeCompatibilityColumnStatements(dialect)},
		{name: "nodes.organization-indexes", statements: organizationIndexStatements(dialect)},
		{name: "metrics.compatibility-columns", mode: migrationStepAddColumn, statements: metricCompatibilityColumnStatements(dialect)},
		{name: "alerts.compatibility-columns", mode: migrationStepAddColumn, statements: alertHistoryCompatibilityColumnStatements(dialect)},
		{name: "k8s.compatibility-columns", mode: migrationStepAddColumn, statements: k8sClusterCompatibilityColumnStatements(dialect)},
		{name: "uptime.compatibility-columns", mode: migrationStepAddColumn, statements: uptimeIncidentCompatibilityColumnStatements(dialect)},
		{name: "uptime.invariants", statements: uptimeIncidentInvariantStatements(dialect)},
		{name: "ai.compatibility-columns", mode: migrationStepAddColumn, statements: aiCompatibilityColumnStatements(dialect)},
		{name: "ai.plan-indexes", statements: aiPlanIndexStatements(dialect)},
		{name: "ai.model-routing-upgrade", statements: aiUpgradeStatements(dialect)},
		{name: "scheduled-tasks.compatibility-columns", mode: migrationStepAddColumn, statements: scheduledTaskCompatibilityColumnStatements(dialect)},
	}
}

func migrateStatements(db *sql.DB, dialect Dialect, statements []string) error {
	if err := executeMigrationStatements(db, statements); err != nil {
		return err
	}
	for _, step := range migrationSteps(dialect) {
		if step.mode == migrationStepAddColumn {
			for _, statement := range step.statements {
				if err := addColumnIfMissing(db, statement); err != nil {
					return err
				}
			}
			continue
		}
		if err := executeMigrationStatements(db, step.statements); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`UPDATE scheduled_tasks SET schedule_type = 'cron' WHERE schedule_type IS NULL OR TRIM(schedule_type) = ''`); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE nodes SET agent_mode = COALESCE(NULLIF(agent_mode, ''), 'normal'), agent_user = COALESCE(agent_user, '')`)
	return err
}

func executeMigrationStatements(db *sql.DB, statements []string) error {
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil && !isIgnorableMigrationError(err) {
			return err
		}
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, statement string) error {
	_, err := db.Exec(statement)
	if err != nil && isIgnorableMigrationError(err) {
		return nil
	}
	return err
}

func isIgnorableMigrationError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate column") || strings.Contains(message, "duplicate key name") ||
		strings.Contains(message, "duplicate foreign key constraint")
}
