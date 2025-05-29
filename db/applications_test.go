package db

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

func TestDedupBackupChanges(t *testing.T) {
	g := gomega.NewWithT(t)

	changes := []ApplicationBackup{
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 16, 6, 83000*1000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 17, 48, 714000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 29, 49, 527000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 31, 32, 215000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 48, 23, 888000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 50, 26, 428000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 9, 59, 33, 44000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 10, 2, 55, 900000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 12, 51, 21, 449000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 12, 52, 43, 736000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "AWS Backup", CreatedAt: time.Date(2025, 5, 28, 16, 36, 26, 147000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 16, 36, 41, 289000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 16, 40, 44, 123000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 16, 58, 55, 474000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 17, 0, 37, 689000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "AWS Backup", CreatedAt: time.Date(2025, 5, 29, 5, 0, 0, 0, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 7, 32, 21, 578000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 7, 36, 24, 205000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 9, 59, 30, 618000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 2, 39, 176000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 3, 35, 310000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 6, 1, 307000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupStarted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 7, 38, 199000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 11, 40, 936000000, time.FixedZone("", 20700))},
	}

	expected := []ApplicationBackup{
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 17, 48, 714000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 31, 32, 215000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 27, 20, 50, 26, 428000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 10, 2, 55, 900000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 12, 52, 43, 736000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "AWS Backup", CreatedAt: time.Date(2025, 5, 28, 16, 36, 26, 147000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 16, 40, 44, 123000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 28, 17, 0, 37, 689000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "AWS Backup", CreatedAt: time.Date(2025, 5, 29, 5, 0, 0, 0, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 7, 36, 24, 205000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31303839-3632-6435-3765-633235636464"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 3, 35, 310000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("31313964-6537-6563-6233-363162396334"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 6, 1, 307000000, time.FixedZone("", 20700))},
		{ConfigID: uuid.MustParse("37356464-6533-3833-3932-626531626231"), ChangeType: "BackupCompleted", Source: "RDS Events", CreatedAt: time.Date(2025, 5, 29, 10, 11, 40, 936000000, time.FixedZone("", 20700))},
	}

	dedupedChanges := dedupBackupChanges(changes)
	g.Expect(dedupedChanges).To(gomega.Equal(expected))
}
