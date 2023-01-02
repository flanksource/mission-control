package snapshot

import (
	"github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
)

type SnapshotContext struct {
	Directory string
	LogStart  string
	LogEnd    string
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
		logger.Errorf("Error dumping components: %v", err)
		return err
	}

	err = dumpConfigs(ctx, r.configIDs)
	if err != nil {
		logger.Errorf("Error dumping configs: %v", err)
		return err
	}

	err = dumpIncidents(ctx, r.incidentIDs)
	if err != nil {
		logger.Errorf("Error dumping incidents: %v", err)
		return err
	}

	return files.Zip(ctx.Directory, ctx.Directory+".zip")
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
	related.componentIDs = append(related.configIDs, componentIDs...)

	for _, componentID := range componentIDs {
		// Fetch related componentIDs
		relatedComponentIDs, err := db.LookupRelatedComponentIDs(componentID, -1)
		if err != nil {
			return related, err
		}
		related.componentIDs = append(related.componentIDs, relatedComponentIDs...)

		// Fetch related incidentIDs
		incidentIDs, err := db.LookupIncidentsByComponent(componentID)
		if err != nil {
			return related, err
		}
		related.incidentIDs = append(related.incidentIDs, incidentIDs...)

		// Fetch related configIDs
		configIDs, err := db.LookupConfigsByComponent(componentID)
		if err != nil {
			return related, err
		}
		related.configIDs = append(related.configIDs, configIDs...)
	}

	return related, nil
}

func fetchRelatedIDsForConfig(configIDs []string) (resource, error) {
	var related resource
	related.configIDs = append(related.configIDs, configIDs...)

	for _, configID := range configIDs {
		relatedConfigIDs, err := db.LookupRelatedConfigIDs(configID, -1)
		if err != nil {
			return related, err
		}
		related.configIDs = append(related.configIDs, relatedConfigIDs...)
	}

	return related, nil
}
