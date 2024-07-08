package push

//import (
//"fmt"
//"io"
//"strings"
////"time"

//"github.com/flanksource/commons/logger"
//"github.com/flanksource/duty"
//dbutils "github.com/flanksource/duty/db"
//"github.com/flanksource/duty/job"
//"github.com/flanksource/duty/models"
//"github.com/flanksource/incident-commander/api"
//)

//func pushTopologiesWithLocation(ctx job.JobRuntime) error {
//var rows []struct {
//ID  string
//URL string
//}
//localFilter := strings.ReplaceAll(duty.LocalFilter, "deleted_at", "topologies.deleted_at")
//localFilter = strings.ReplaceAll(localFilter, "agent_id", "topologies.agent_id")
//if err := ctx.DB().Model(&models.Topology{}).
//Select("topologies.id as topology_id", "spec->'pushLocation'->>'url' as url", "components.id as id").
//Joins("LEFT JOIN components ON  components.topology_id = topologies.id").
//Where(localFilter).
//Where("spec ? 'pushLocation'").Where("components.parent_id IS NULL").
//Scan(&rows).Error; err != nil {
//return fmt.Errorf("error querying topologies with location: %w", dbutils.ErrorDetails(err))
//}

////time.Sleep(time.Minute * 5)

//// SELECT component id from topology id

//var agentName string
//if api.UpstreamConf.Valid() {
//agentName = api.UpstreamConf.AgentName
//}

//logger.Infof("GOT ROWS = %d", len(rows))
//for _, row := range rows {
//if err != nil {
//ctx.History.AddErrorf("error querying topology tree: %v", err)
//continue
//}

//// TODO: Figure out auth

//if agentName != "" {
//req.QueryParam("agentName", agentName)
//}

//// TODO: Handle error with 0 components
//logger.Infof("PUSH URL Is %v", row)
//resp, err := req.
//if err != nil {
//ctx.History.AddErrorf("error pushing topology tree to location[%s]: %v", endpoint, err)
//logger.Infof("YASH ERROR IS %v", err)
//fmt.Printf("YASH ERROR IS %v", err)
//continue
//}

//if !resp.IsOK() {
//respBody, _ := io.ReadAll(resp.Body)
//ctx.History.AddErrorf("non 2xx response for pushing topology tree to location[%s]: %s, %s", row.URL, resp.Status, string(respBody))
//logger.Infof("YASH2 ERROR IS %v", resp.Body)
//fmt.Printf("YASH2 ERROR IS %v", resp.Body)
//continue
//}

//logger.Infof("YASH3 Resp is %v", resp.Status)
//fmt.Printf("YASH3 Resp is %v", resp.Status)
//ctx.History.IncrSuccess()
//}

//return nil
//}

//// PushTopologiesWithLocation periodically pulls playbook actions to run
//var PushTopologiesWithLocation = &job.Job{
//Name:       "PushTopologiesWithLocation",
//Schedule:   "@every 5m",
//Retention:  job.RetentionFew,
//JobHistory: true,
//RunNow:     true,
//Singleton:  true,
//Fn:         pushTopologiesWithLocation,
//}
