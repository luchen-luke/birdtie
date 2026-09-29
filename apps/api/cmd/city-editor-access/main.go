package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ticketPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{3,79}$`)

func main() {
	var action, cityID, accountID, operatorID, role, ticket string
	flag.StringVar(&action, "action", "", "grant or revoke")
	flag.StringVar(&cityID, "city", "", "published City ID")
	flag.StringVar(&accountID, "account", "", "target Account UUID")
	flag.StringVar(&operatorID, "operator", "", "active operator Account UUID")
	flag.StringVar(&role, "role", "", "contributor or reviewer, required for grant")
	flag.StringVar(&ticket, "ticket", "", "non-sensitive approval ticket ID")
	flag.Parse()
	if (action != "grant" && action != "revoke") || cityID == "" ||
		accountID == "" || operatorID == "" || !ticketPattern.MatchString(ticket) ||
		(action == "grant" && role != "contributor" && role != "reviewer") ||
		(action == "revoke" && role != "") || flag.NArg() != 0 {
		log.Fatal("invalid arguments: provide action, city, account, operator, ticket and role for grant")
	}
	databaseURL := os.Getenv("BIRDTIE_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("BIRDTIE_DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectCtx, databaseURL)
	if err != nil {
		log.Fatal("invalid database configuration")
	}
	defer pool.Close()
	if err := pool.Ping(connectCtx); err != nil {
		log.Fatal("database unavailable")
	}
	workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := changeMembership(workCtx, pool, action, cityID, accountID, operatorID, role, ticket); err != nil {
		log.Fatalf("city editor access: %v", err)
	}
	fmt.Printf("%s recorded for account %s in city %s (ticket %s)\n", action, accountID, cityID, ticket)
}

func changeMembership(ctx context.Context, pool *pgxpool.Pool, action, cityID, accountID, operatorID, role, ticket string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM cities c
        JOIN accounts target ON target.id = $2 AND target.status = 'active'
        JOIN accounts operator ON operator.id = $3 AND operator.status = 'active'
        WHERE c.id = $1 AND c.publication_status = 'published'
    )`, cityID, accountID, operatorID).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return errors.New("published City and active target/operator Accounts are required")
	}
	if action == "grant" {
		_, err = tx.Exec(ctx, `INSERT INTO city_editor_memberships (
            city_id, account_id, role, state, granted_by
        ) VALUES ($1, $2, $3, 'active', $4)
        ON CONFLICT (city_id, account_id) DO UPDATE
        SET role = EXCLUDED.role, state = 'active', granted_by = EXCLUDED.granted_by,
            created_at = now(), revoked_at = NULL`,
			cityID, accountID, role, operatorID)
	} else {
		var revokedAccount string
		err = tx.QueryRow(ctx, `UPDATE city_editor_memberships
            SET state = 'revoked', revoked_at = now()
            WHERE city_id = $1 AND account_id = $2 AND state = 'active'
            RETURNING account_id`, cityID, accountID).Scan(&revokedAccount)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("active membership not found")
		}
	}
	if err != nil {
		return err
	}
	auditAction := action
	if action == "grant" {
		auditAction += "_" + role
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, $2, 'city_editor_membership', $3, 'allowed', $4)`,
		operatorID, auditAction, cityID+"/"+accountID, "operator_ticket/"+ticket)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
