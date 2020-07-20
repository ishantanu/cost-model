package costmodel

import (
	"errors"
	"fmt"

	costAnalyzerCloud "github.com/kubecost/cost-model/pkg/cloud"
	"github.com/kubecost/cost-model/pkg/log"
	"github.com/kubecost/cost-model/pkg/prom"
	"github.com/kubecost/cost-model/pkg/util"
)

func parsePodLabels(qrs *prom.QueryResults) (map[string]map[string]string, error) {
	podLabels := map[string]map[string]string{}

	for _, result := range qrs.Results {
		pod, err := result.GetString("pod")
		if err != nil {
			return podLabels, errors.New("missing pod field")
		}

		if _, ok := podLabels[pod]; ok {
			podLabels[pod] = result.GetLabels()
		} else {
			podLabels[pod] = map[string]string{}
			podLabels[pod] = result.GetLabels()
		}
	}

	return podLabels, nil
}

func GetPVInfo(qrs *prom.QueryResults, defaultClusterID string) (map[string]*PersistentVolumeClaimData, error) {
	toReturn := map[string]*PersistentVolumeClaimData{}

	for _, val := range qrs.Results {
		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		pvcName, err := val.GetString("persistentvolumeclaim")
		if err != nil {
			return toReturn, err
		}

		volumeName, err := val.GetString("volumename")
		if err != nil {
			log.Debugf("Unfulfilled claim %s: volumename field does not exist in data result vector", pvcName)
			volumeName = ""
		}

		pvClass, err := val.GetString("storageclass")
		if err != nil {
			// TODO: We need to look up the actual PV and PV capacity. For now just proceed with "".
			log.Warningf("Storage Class not found for claim \"%s/%s\".", ns, pvcName)
			pvClass = ""
		}

		key := fmt.Sprintf("%s,%s,%s", ns, pvcName, clusterID)
		toReturn[key] = &PersistentVolumeClaimData{
			Class:      pvClass,
			Claim:      pvcName,
			Namespace:  ns,
			ClusterID:  clusterID,
			VolumeName: volumeName,
			Values:     val.Values,
		}
	}

	return toReturn, nil
}

func GetPVAllocationMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string][]*PersistentVolumeClaimData, error) {
	toReturn := map[string][]*PersistentVolumeClaimData{}

	for _, val := range qrs.Results {
		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		pod, err := val.GetString("pod")
		if err != nil {
			return toReturn, err
		}

		pvcName, err := val.GetString("persistentvolumeclaim")
		if err != nil {
			return toReturn, err
		}

		pvName, err := val.GetString("persistentvolume")
		if err != nil {
			log.Warningf("persistentvolume field does not exist for pv %s", pvcName) // This is possible for an unfulfilled claim
			continue
		}

		key := fmt.Sprintf("%s,%s,%s", ns, pod, clusterID)
		pvcData := &PersistentVolumeClaimData{
			Class:      "",
			Claim:      pvcName,
			Namespace:  ns,
			ClusterID:  clusterID,
			VolumeName: pvName,
			Values:     val.Values,
		}

		toReturn[key] = append(toReturn[key], pvcData)
	}

	return toReturn, nil
}

func GetPVCostMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]*costAnalyzerCloud.PV, error) {
	toReturn := map[string]*costAnalyzerCloud.PV{}

	for _, val := range qrs.Results {
		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		volumeName, err := val.GetString("volumename")
		if err != nil {
			return toReturn, err
		}

		key := fmt.Sprintf("%s,%s", volumeName, clusterID)
		toReturn[key] = &costAnalyzerCloud.PV{
			Cost: fmt.Sprintf("%f", val.Values[0].Value),
		}
	}

	return toReturn, nil
}

func GetNamespaceLabelsMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]map[string]string, error) {
	toReturn := make(map[string]map[string]string)

	for _, val := range qrs.Results {
		// We want Namespace and ClusterID for key generation purposes
		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		nsKey := ns + "," + clusterID
		if nsLabels, ok := toReturn[nsKey]; ok {
			for k, v := range val.GetLabels() {
				nsLabels[k] = v // override with more recently assigned if we changed labels within the window.
			}
		} else {
			toReturn[nsKey] = val.GetLabels()
		}
	}
	return toReturn, nil
}

func GetPodLabelsMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]map[string]string, error) {
	toReturn := make(map[string]map[string]string)

	for _, val := range qrs.Results {
		// We want Pod, Namespace and ClusterID for key generation purposes
		pod, err := val.GetString("pod")
		if err != nil {
			return toReturn, err
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		nsKey := ns + "," + pod + "," + clusterID
		if labels, ok := toReturn[nsKey]; ok {
			newlabels := val.GetLabels()
			for k, v := range newlabels {
				labels[k] = v
			}
		} else {
			toReturn[nsKey] = val.GetLabels()
		}
	}

	return toReturn, nil
}

func GetStatefulsetMatchLabelsMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]map[string]string, error) {
	toReturn := make(map[string]map[string]string)

	for _, val := range qrs.Results {
		// We want Statefulset, Namespace and ClusterID for key generation purposes
		ss, err := val.GetString("statefulSet")
		if err != nil {
			return toReturn, err
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		nsKey := ns + "," + ss + "," + clusterID
		toReturn[nsKey] = val.GetLabels()
	}

	return toReturn, nil
}

func GetPodDaemonsetsWithMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]string, error) {
	toReturn := make(map[string]string)

	for _, val := range qrs.Results {
		ds, err := val.GetString("owner_name")
		if err != nil {
			return toReturn, err
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		pod, err := val.GetString("pod")
		if err != nil {
			return toReturn, err
		}

		nsKey := ns + "," + pod + "," + clusterID
		toReturn[nsKey] = ds
	}

	return toReturn, nil
}

func GetDeploymentMatchLabelsMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]map[string]string, error) {
	toReturn := make(map[string]map[string]string)

	for _, val := range qrs.Results {
		// We want Deployment, Namespace and ClusterID for key generation purposes
		deployment, err := val.GetString("deployment")
		if err != nil {
			return toReturn, err
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		nsKey := ns + "," + deployment + "," + clusterID
		toReturn[nsKey] = val.GetLabels()
	}

	return toReturn, nil
}

func GetServiceSelectorLabelsMetrics(qrs *prom.QueryResults, defaultClusterID string) (map[string]map[string]string, error) {
	toReturn := make(map[string]map[string]string)

	for _, val := range qrs.Results {
		// We want Service, Namespace and ClusterID for key generation purposes
		service, err := val.GetString("service")
		if err != nil {
			return toReturn, err
		}

		ns, err := val.GetString("namespace")
		if err != nil {
			return toReturn, err
		}

		clusterID, err := val.GetString("cluster_id")
		if clusterID == "" {
			clusterID = defaultClusterID
		}

		nsKey := ns + "," + service + "," + clusterID
		toReturn[nsKey] = val.GetLabels()
	}

	return toReturn, nil
}
func getCost(qrs *prom.QueryResults) (map[string][]*util.Vector, error) {
	costs := map[string][]*util.Vector{}

	for _, result := range qrs.Results {
		instance, err := result.GetString("instance")
		if err != nil {
			return costs, err
		}

		costs[instance] = result.Values
	}

	return costs, nil
}

// TODO niko/prom retain message:
// normalization data is empty: time window may be invalid or kube-state-metrics or node-exporter may not be running
func getNormalization(qrs *prom.QueryResults) (float64, error) {
	return qrs.GetFirstValue()
}

// TODO niko/prom retain message:
// normalization data is empty: time window may be invalid or kube-state-metrics or node-exporter may not be running
func getNormalizations(qrs *prom.QueryResults) ([]*util.Vector, error) {
	if len(qrs.Results) == 0 {
		return nil, prom.NoDataErr
	}

	return qrs.Results[0].Values, nil
}

func GetContainerMetricVector(qrs *prom.QueryResults, normalize bool, normalizationValue float64, defaultClusterID string) (map[string][]*util.Vector, error) {
	containerData := make(map[string][]*util.Vector)
	for _, val := range qrs.Results {
		containerMetric, err := NewContainerMetricFromPrometheus(val.Metric, defaultClusterID)
		if err != nil {
			return nil, err
		}

		if normalize && normalizationValue != 0 {
			for _, v := range val.Values {
				v.Value = v.Value / normalizationValue
			}
		}
		containerData[containerMetric.Key()] = val.Values
	}
	return containerData, nil
}

func GetContainerMetricVectors(qrs *prom.QueryResults, defaultClusterID string) (map[string][]*util.Vector, error) {
	containerData := make(map[string][]*util.Vector)
	for _, val := range qrs.Results {
		containerMetric, err := NewContainerMetricFromPrometheus(val.Metric, defaultClusterID)
		if err != nil {
			return nil, err
		}
		containerData[containerMetric.Key()] = val.Values
	}
	return containerData, nil
}

func GetNormalizedContainerMetricVectors(qrs *prom.QueryResults, normalizationValues []*util.Vector, defaultClusterID string) (map[string][]*util.Vector, error) {
	containerData := make(map[string][]*util.Vector)
	for _, val := range qrs.Results {
		containerMetric, err := NewContainerMetricFromPrometheus(val.Metric, defaultClusterID)
		if err != nil {
			return nil, err
		}
		containerData[containerMetric.Key()] = util.NormalizeVectorByVector(val.Values, normalizationValues)
	}
	return containerData, nil
}
