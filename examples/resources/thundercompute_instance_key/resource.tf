resource "thundercompute_instance_key" "additional" {
  instance_id = thundercompute_instance.training.id
  public_key  = file("~/.ssh/id_ed25519.pub")
}
