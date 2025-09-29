output "cluster_name" {
  description = "Name of the GKE cluster"
  value       = google_container_cluster.wasm_pep_demo.name
}

output "cluster_endpoint" {
  description = "Endpoint for GKE cluster"
  value       = google_container_cluster.wasm_pep_demo.endpoint
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "CA certificate for GKE cluster"
  value       = google_container_cluster.wasm_pep_demo.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

output "region" {
  description = "GCP region"
  value       = var.region
}

output "project_id" {
  description = "GCP project ID"
  value       = var.project_id
}

output "artifact_registry_url" {
  description = "Artifact Registry URL for container images"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.container_repo.repository_id}"
}

output "vpc_network" {
  description = "VPC network name"
  value       = google_compute_network.vpc.name
}

output "get_credentials_command" {
  description = "Command to get GKE cluster credentials"
  value       = "gcloud container clusters get-credentials ${google_container_cluster.wasm_pep_demo.name} --region=${var.region} --project=${var.project_id}"
}