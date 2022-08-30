-- +goose Up
-- +goose StatementBegin
---


ALTER TABLE team_components ADD selector_id TEXT;
ALTER TABLE team_components ADD CONSTRAINT team_component_unique UNIQUE (team_id, component_id, selector_id)

-- +goose StatementEnd

-- +goose Down