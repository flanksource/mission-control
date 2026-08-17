package cmd

import (
	gocontext "context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/flanksource/clicky"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/connection"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/sdk"
)

// localConnectionOps implements clientcmd.LocalConnectionOps using a direct
// database connection and the local connection-testing machinery. It is only
// wired into the full mission-control binary; faro leaves it unset.
type localConnectionOps struct{}

func (localConnectionOps) LoadAWSProfile(flags *clientcmd.ConnectionFlags) error {
	cfg, err := config.LoadDefaultConfig(gocontext.Background(),
		config.WithSharedConfigProfile(flags.FromProfile),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS profile %q: %w", flags.FromProfile, err)
	}

	creds, err := cfg.Credentials.Retrieve(gocontext.Background())
	if err != nil {
		return fmt.Errorf("failed to retrieve credentials from AWS profile %q: %w", flags.FromProfile, err)
	}

	if flags.AccessKey == "" {
		flags.AccessKey = creds.AccessKeyID
	}
	if flags.SecretKey == "" {
		flags.SecretKey = creds.SecretAccessKey
	}
	if creds.SessionToken != "" && flags.SessionToken == "" {
		flags.SessionToken = creds.SessionToken
	}
	if flags.Region == "" && cfg.Region != "" {
		flags.Region = cfg.Region
	}
	return nil
}

func (localConnectionOps) AddViaDB(flags *clientcmd.ConnectionFlags, conn *clientapi.Connection) error {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
	defer stop()
	modelConn := connectionToModel(*conn)

	var existing models.Connection
	err = ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", flags.Name, flags.Namespace).First(&existing).Error
	isUpdate := false
	if err == nil {
		isUpdate = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing connection: %w", err)
	}

	if isUpdate {
		modelConn.ID = existing.ID
		modelConn.CreatedAt = existing.CreatedAt
	} else {
		modelConn.ID = uuid.New()
	}

	if flags.Test {
		hydrated, err := ctx.HydrateConnection(&modelConn)
		if err != nil {
			return fmt.Errorf("failed to hydrate connection: %w", err)
		}
		result, err := connection.Test(ctx, hydrated)
		if err != nil {
			clicky.MustPrint(result, clicky.Flags.FormatOptions)
			return fmt.Errorf("connection test failed: %w", err)
		}
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		fmt.Println("\nConnection test passed")
	}

	if err := ctx.DB().Save(&modelConn).Error; err != nil {
		return fmt.Errorf("failed to save connection: %w", err)
	}

	if isUpdate {
		fmt.Printf("Connection '%s' updated in namespace '%s'\n", flags.Name, flags.Namespace)
	} else {
		fmt.Printf("Connection '%s' created in namespace '%s'\n", flags.Name, flags.Namespace)
	}

	return nil
}

func (localConnectionOps) TestSaved(name, namespace string, overrides *clientcmd.ConnectionFlags) (any, error) {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return nil, err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
	defer stop()

	var conn models.Connection
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).First(&conn).Error; err != nil {
		return nil, fmt.Errorf("connection %s/%s not found: %w", namespace, name, err)
	}

	if overrides != nil {
		applyConnectionOverrides(&conn, overrides)
	}

	if clicky.Flags.LevelCount >= 1 {
		clientcmd.PrintConnectionState(connectionFromModel(conn), clicky.Flags.LevelCount)
	}

	hydrated, err := ctx.HydrateConnection(&conn)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}

	result, err := connection.Test(ctx, hydrated)
	if err != nil {
		return result, fmt.Errorf("connection test failed: %w", err)
	}

	return result, nil
}

func (localConnectionOps) TestTransient(flags *clientcmd.ConnectionFlags) (any, error) {
	if flags.FromProfile != "" {
		if err := (localConnectionOps{}).LoadAWSProfile(flags); err != nil {
			return nil, err
		}
	}

	conn, err := clientcmd.BuildConnectionFromFlags(flags)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection: %w", err)
	}

	modelConn := connectionToModel(conn)
	return hydrateAndTest(&modelConn)
}

func (localConnectionOps) TestFile(filename string) (any, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var crd v1.Connection
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if crd.Kind != "" && crd.Kind != "Connection" {
		return nil, fmt.Errorf("expected Kind=Connection, got %s", crd.Kind)
	}

	conn, err := db.ConnectionFromCRD(&crd)
	if err != nil {
		return nil, err
	}

	return hydrateAndTest(&conn)
}

func (localConnectionOps) GetConnection(name, namespace string) (*clientapi.Connection, error) {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return nil, err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
	defer stop()

	var conn models.Connection
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("connection %s/%s not found: %w", namespace, name, sdk.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to load connection %s/%s: %w", namespace, name, err)
	}
	result := connectionFromModel(conn)
	return &result, nil
}

func (localConnectionOps) SaveConnection(conn *clientapi.Connection) error {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
	defer stop()

	modelConn := connectionToModel(*conn)
	if modelConn.ID == uuid.Nil {
		modelConn.ID = uuid.New()
	}
	return ctx.DB().Save(&modelConn).Error
}

func connectionFromModel(conn models.Connection) clientapi.Connection {
	return clientapi.Connection{
		ID:          conn.ID,
		Name:        conn.Name,
		Namespace:   conn.Namespace,
		Source:      conn.Source,
		Type:        conn.Type,
		URL:         conn.URL,
		Username:    conn.Username,
		Password:    conn.Password,
		Properties:  map[string]string(conn.Properties),
		Certificate: conn.Certificate,
		InsecureTLS: conn.InsecureTLS,
		CreatedAt:   conn.CreatedAt,
		UpdatedAt:   conn.UpdatedAt,
		CreatedBy:   conn.CreatedBy,
	}
}

func connectionToModel(conn clientapi.Connection) models.Connection {
	return models.Connection{
		ID:          conn.ID,
		Name:        conn.Name,
		Namespace:   conn.Namespace,
		Source:      conn.Source,
		Type:        conn.Type,
		URL:         conn.URL,
		Username:    conn.Username,
		Password:    conn.Password,
		Properties:  map[string]string(conn.Properties),
		Certificate: conn.Certificate,
		InsecureTLS: conn.InsecureTLS,
		CreatedAt:   conn.CreatedAt,
		UpdatedAt:   conn.UpdatedAt,
		CreatedBy:   conn.CreatedBy,
	}
}

func hydrateAndTest(conn *models.Connection) (any, error) {
	ctx := context.NewContext(gocontext.Background())
	hydrated, err := ctx.HydrateConnection(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}

	result, err := connection.Test(ctx, hydrated)
	if err != nil {
		return result, fmt.Errorf("connection test failed: %w", err)
	}

	return result, nil
}

func applyConnectionOverrides(conn *models.Connection, flags *clientcmd.ConnectionFlags) {
	if flags.URL != "" {
		conn.URL = flags.URL
	}
	if flags.Username != "" {
		conn.Username = flags.Username
	}
	if flags.Password != "" {
		conn.Password = flags.Password
	}
	if flags.Certificate != "" {
		conn.Certificate = flags.Certificate
	}
	if flags.InsecureTLS {
		conn.InsecureTLS = true
	}
}

func init() {
	clientcmd.LocalConnections = localConnectionOps{}
	clientcmd.ConnectionAdd.PersistentPreRun = PreRun
	clientcmd.ConnectionTest.PersistentPreRun = PreRun
}
