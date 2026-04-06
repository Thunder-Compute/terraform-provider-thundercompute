resource "thundercompute_snapshot" "checkpoint" {
  instance_id = thundercompute_instance.training.id
  name        = "training-checkpoint-v1"
}
