package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"gorm.io/gorm"
)

func getAgent(name string) (*models.Agent, error) {
	t := models.Agent{Name: name}
	tx := Gorm.Where(t).First(&t)
	return &t, tx.Error
}

func createAgent(name string) (*models.Agent, error) {
	a := models.Agent{Name: name}
	tx := Gorm.Create(&a)
	return &a, tx.Error
}

func GetOrCreateAgent(name string) (*models.Agent, error) {
	a, err := getAgent(name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			newAgent, err := createAgent(name)
			if err != nil {
				return nil, fmt.Errorf("failed to create agent: %w", err)
			}
			return newAgent, nil
		}
		return nil, err
	}

	return a, nil
}
