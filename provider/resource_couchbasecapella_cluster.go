package provider

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	couchbasecapella "github.com/couchbaselabs/couchbase-cloud-go-client"
)

func resourceCouchbaseCapellaCluster() *schema.Resource {
	return &schema.Resource{
		Description: "Manage Couchbase Capella clusters.",

		CreateContext: resourceCouchbaseCapellaClusterCreate,
		ReadContext:   resourceCouchbaseCapellaClusterRead,
		UpdateContext: resourceCouchbaseCapellaClusterUpdate,
		DeleteContext: resourceCouchbaseCapellaClusterDelete,

		Schema: map[string]*schema.Schema{
			"id": {
				Description: "Cluster's id.",
				Type:        schema.TypeString,
				ForceNew:    true,
				Computed:    true,
			},
			"name": {
				Description: "Cluster's name.",
				Type:        schema.TypeString,
				ForceNew:    true,
				Required:    true,
			},
			"cloud_id": {
				Description: "Cloud's Id.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"project_id": {
				Description: "Project's Id.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"servers": {
				Description: "Cluster servers configuration",
				Type:        schema.TypeSet,
				Required:    true,
				ForceNew:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"size": {
							Type:        schema.TypeInt,
							Description: "Number of nodes",
							Required:    true,
						},
						"services": {
							Type:        schema.TypeList,
							Description: "Services",
							Required:    true,
							MinItems:    1,
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
						},
						"aws": {
							Description: "Aws configuration.",
							Type:        schema.TypeSet,
							Optional:    true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"instance_size": {
										Description: "Aws instance.",
										Type:        schema.TypeString,
										Required:    true,
									},
									"ebs_size_gib": {
										Description: "Aws size(Gb).",
										Type:        schema.TypeInt,
										Required:    true,
									},
								},
							},
						},
						"azure": {
							Description: "Azure configuration.",
							Type:        schema.TypeSet,
							Optional:    true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"instance_size": {
										Description: "Azure instance.",
										Type:        schema.TypeString,
										Required:    true,
									},
									"volume_type": {
										Description: "Azure size(Gb).",
										Type:        schema.TypeString,
										Required:    true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func resourceCouchbaseCapellaClusterCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*couchbasecapella.APIClient)
	auth := getAuth(ctx)

	clusterName := d.Get("name").(string)
	cloudId := d.Get("cloud_id").(string)
	projectId := d.Get("project_id").(string)

	newClusterRequest := *couchbasecapella.NewCreateClusterRequest(clusterName, cloudId, projectId)

	// Get The cloud
	cloud, resp, err := client.CloudsApi.CloudsShow(auth, cloudId).Execute()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return diag.FromErr(fmt.Errorf("404: the cloud doesn't exist. Please verify your cloud_id"))
		}
		return diag.FromErr(err)
	}
	providerName := string(cloud.Provider)
	// add Servers + Check servers Vs Cloud provider
	if servers, ok := d.GetOk("servers"); ok {
		// check server providers
		providers := getServersProvider(servers.(*schema.Set))
		if len(providers) > 1 {
			return diag.FromErr(fmt.Errorf("cluster's server should be the same as the cloud provider"))
		}
		if len(providers) == 1 && !Has(providers, providerName) {
			return diag.FromErr(fmt.Errorf("cluster's server should be the same as the cloud provider"))
		}
		newClusterRequest.SetServers(expandServersSet(servers.(*schema.Set)))
	}

	response, error := client.ClustersApi.ClustersCreate(auth).CreateClusterRequest(newClusterRequest).Execute()
	if error != nil {
		return diag.FromErr(error)
	}
	defer response.Body.Close()

	// TODO: need to be changed after cloud api fix!
	location := string(response.Header.Get("Location"))
	urlparts := strings.Split(location, "/")
	clusterId := urlparts[len(urlparts)-1]
	d.SetId(clusterId)

	return resourceCouchbaseCapellaClusterRead(ctx, d, meta)
}

func resourceCouchbaseCapellaClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	log.Print("[INFO] READ CLUSTER ID : ", d.Get("id").(string))
	client := meta.(*couchbasecapella.APIClient)
	auth := getAuth(ctx)
	clusterId := d.Get("id").(string)

	cluster, resp, err := client.ClustersApi.ClustersShow(auth, clusterId).Execute()

	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	if err := d.Set("name", cluster.Name); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceCouchbaseCapellaClusterUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	// use the meta value to retrieve your client from the provider configure method
	// client := meta.(*apiClient)

	return diag.Errorf("not implemented")
}

func resourceCouchbaseCapellaClusterDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*couchbasecapella.APIClient)
	auth := getAuth(ctx)

	clusterId := d.Get("id").(string)

	statusResp, _, err := client.ClustersApi.ClustersStatus(auth, clusterId).Execute()
	if err != nil {
		return diag.FromErr(err)
	}
	if statusResp.Status != "ready" {
		return diag.Errorf("Cluster is not ready to be deleted. Cluster Status: %s", statusResp.Status)
	}

	_, err2 := client.ClustersApi.ClustersDelete(auth, clusterId).Execute()
	if err2 != nil {
		return diag.FromErr(err2)
	}
	return nil
}

func expandServersSet(servers *schema.Set) []couchbasecapella.Server {
	result := make([]couchbasecapella.Server, servers.Len())

	for i, value := range servers.List() {
		v := value.(map[string]interface{})
		result[i] = createServer(v)
	}

	return result
}

func expandServiceList(services []interface{}) (res []couchbasecapella.CouchbaseServices) {
	for _, v := range services {
		res = append(res, couchbasecapella.CouchbaseServices(v.(string)))
	}

	return res
}

func getServersProvider(servers *schema.Set) []string {
	providers := make([]string, 0)

	for _, value := range servers.List() {
		server := value.(map[string]interface{})
		for k, v := range server {
			if k == "aws" && len(v.(*schema.Set).List()) > 0 {
				if !Has(providers, "aws") {
					providers = append(providers, "aws")
				}
			}
			if k == "azure" && len(v.(*schema.Set).List()) > 0 {
				if !Has(providers, "azure") {
					providers = append(providers, "azure")
				}
			}
		}
	}
	return providers
}

func createServer(v map[string]interface{}) couchbasecapella.Server {
	var server couchbasecapella.Server
	for _, awss := range v["aws"].(*schema.Set).List() {
		aws, ok := awss.(map[string]interface{})
		if ok {
			server = couchbasecapella.Server{
				Size:     int32(v["size"].(int)),
				Services: expandServiceList(v["services"].([]interface{})),
				Aws: &couchbasecapella.ServerAws{
					InstanceSize: couchbasecapella.AwsInstances(aws["instance_size"].(string)),
					EbsSizeGib:   int32(aws["ebs_size_gib"].(int)),
				},
			}
		}
	}
	for _, azures := range v["azure"].(*schema.Set).List() {
		azure, ok := azures.(map[string]interface{})
		if ok {
			server = couchbasecapella.Server{
				Size:     int32(v["size"].(int)),
				Services: expandServiceList(v["services"].([]interface{})),
				Azure: &couchbasecapella.ServerAzure{
					InstanceSize: couchbasecapella.AzureInstances(azure["instance_size"].(string)),
					VolumeType:   couchbasecapella.AzureVolumeTypes(azure["volume_type"].(string)),
				},
			}
		}
	}

	return server
}
