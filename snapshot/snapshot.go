package snapshot

import (
	"database/sql"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
)

type SnapshotContext struct {
	Directory string
	LogStart  string
}

type resource struct {
	componentIDs []string
	configIDs    []string
	incidentIDs  []string
}

func (src *resource) merge(dst resource) {
	src.componentIDs = append(src.componentIDs, dst.componentIDs...)
	src.configIDs = append(src.configIDs, dst.configIDs...)
	src.incidentIDs = append(src.incidentIDs, dst.incidentIDs...)
}

func (r *resource) dedup() {
	r.componentIDs = utils.Dedup(r.componentIDs)
	r.configIDs = utils.Dedup(r.configIDs)
	r.incidentIDs = utils.Dedup(r.incidentIDs)
}

func (r *resource) dump(ctx SnapshotContext) error {
	// Dedup since there maybe repetitions
	r.dedup()

	err := dumpComponents(ctx, r.componentIDs)
	if err != nil {
		return err
	}

	err = dumpConfigs(ctx, r.configIDs)
	if err != nil {
		return err
	}

	err = dumpIncidents(ctx, r.incidentIDs)
	if err != nil {
		return err
	}

	return utils.Zip(ctx.Directory, ctx.Directory+".zip")
}

func topologySnapshot(ctx SnapshotContext, componentID string, related bool) error {
	var resources resource
	componentIDs := []string{componentID}
	resources.componentIDs = append(resources.componentIDs, componentIDs...)
	if related {
		relatedResources, err := fetchRelatedIDsForComponent(componentIDs)
		if err != nil {
			return err
		}
		resources.merge(relatedResources)

		relatedConfigResources, err := fetchRelatedIDsForConfig(relatedResources.configIDs)
		if err != nil {
			return err
		}
		resources.merge(relatedConfigResources)
	}

	return resources.dump(ctx)
}

func incidentSnapshot(ctx SnapshotContext, incidentID string) error {
	var resources resource
	resources.incidentIDs = []string{incidentID}
	return resources.dump(ctx)
}

func configSnapshot(ctx SnapshotContext, configID string, related bool) error {
	var resources resource
	configIDs := []string{configID}
	resources.configIDs = append(resources.configIDs, configIDs...)
	if related {
		relatedResources, err := fetchRelatedIDsForConfig(resources.configIDs)
		if err != nil {
			return err
		}
		resources.merge(relatedResources)
	}

	return resources.dump(ctx)
}

func fetchRelatedIDsForComponent(componentIDs []string) (resource, error) {
	var related resource

	// Fetch related relatedComponentIDs
	var relatedComponentIDs []string
	err := db.Gorm.Raw(`
        WITH RECURSIVE children AS (
            SELECT id as child, parent_id as parent
            FROM components
            WHERE parent_id is null
            UNION ALL
            SELECT m.id, COALESCE(c.parent,m.parent_id) 
            FROM components m
            JOIN children c ON m.parent_id = c.child
        )
        SELECT child FROM children WHERE parent IN (?)
    `, componentIDs).Scan(&relatedComponentIDs).Error
	if err != nil {
		return related, err
	}
	related.componentIDs = append(related.componentIDs, relatedComponentIDs...)

	relatedComponentIDs = []string{}
	err = db.Gorm.Raw(`
        SELECT relationship_id  FROM component_relationships WHERE component_id IN (@componentIDs)
        UNION
        SELECT component_id FROM component_relationships WHERE relationship_id IN (@componentIDs)
    `, sql.Named("componentIDs", componentIDs)).Scan(&relatedComponentIDs).Error
	if err != nil {
		return related, err
	}
	related.componentIDs = append(related.componentIDs, relatedComponentIDs...)

	// Fetch related incidentIDs
	var incidentIDs []string
	err = db.Gorm.Raw(`
        SELECT id FROM incidents WHERE id IN (
            SELECT incident_id FROM hypotheses WHERE id IN (
                SELECT hypothesis_id FROM evidences WHERE component_id IN (?)
            )
        )`, componentIDs).Scan(&incidentIDs).Error
	if err != nil {
		return related, err
	}

	related.incidentIDs = append(related.incidentIDs, incidentIDs...)

	// Fetch related configIDs
	var configIDs []string
	err = db.Gorm.Raw(`
        SELECT config_id FROM config_component_relationships WHERE component_id IN (?)
    `, componentIDs).Scan(&configIDs).Error
	if err != nil {
		return related, err
	}

	related.configIDs = append(related.configIDs, configIDs...)

	return related, nil
}

func fetchRelatedIDsForConfig(configIDs []string) (resource, error) {
	var related resource
	related.configIDs = append(related.configIDs, configIDs...)

	var relatedConfigIDs []string
	err := db.Gorm.Raw(`
        WITH RECURSIVE children AS (
            SELECT id as child, parent_id as parent
            FROM config_items
            WHERE parent_id is null
            UNION ALL
            SELECT m.id, COALESCE(c.parent,m.parent_id) 
            FROM config_items m
            JOIN children c ON m.parent_id = c.child
        )
        SELECT child FROM children WHERE parent IN (?)
    `, configIDs).Scan(&relatedConfigIDs).Error
	if err != nil {
		return related, err
	}
	related.configIDs = append(related.configIDs, relatedConfigIDs...)

	// Fetch config relationships
	relatedConfigIDs = []string{}
	err = db.Gorm.Raw(`
        SELECT related_id  FROM config_relationships WHERE config_id IN (@configIDs)
        UNION
        SELECT config_id FROM config_relationships WHERE related_id IN (@configIDs)
    `, sql.Named("configIDs", configIDs)).Scan(&relatedConfigIDs).Error
	if err != nil {
		return related, err
	}

	related.configIDs = append(related.configIDs, relatedConfigIDs...)
	return related, nil
}
