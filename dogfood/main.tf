terraform {
  required_providers {
    wirtuald = {
      source = "coder/wirtuald"
    }
  }
  backend "gcs" {
    bucket = "coder-dogfood-tf-state"
  }
}

data "wirtuald_organization" "default" {
  is_default = true
}

data "wirtuald_user" "machine" {
  username = "machine"
}

variable "WIRTUAL_TEMPLATE_NAME" {
  type = string
}

variable "WIRTUAL_TEMPLATE_VERSION" {
  type = string
}

variable "WIRTUAL_TEMPLATE_DIR" {
  type = string
}

variable "WIRTUAL_TEMPLATE_MESSAGE" {
  type = string
}

resource "wirtuald_template" "dogfood" {
  name            = var.WIRTUAL_TEMPLATE_NAME
  display_name    = "Write Coder on Coder"
  description     = "The template to use when developing Coder on Coder!"
  icon            = "/emojis/1f3c5.png"
  organization_id = "703f72a1-76f6-4f89-9de6-8a3989693fe5"
  versions = [
    {
      name      = var.WIRTUAL_TEMPLATE_VERSION
      message   = var.WIRTUAL_TEMPLATE_MESSAGE
      directory = var.WIRTUAL_TEMPLATE_DIR
      active    = true
    }
  ]
  acl = {
    groups = [{
      id   = data.wirtuald_organization.default.id
      role = "use"
    }]
    users = [{
      id   = data.wirtuald_user.machine.id
      role = "admin"
    }]
  }
  activity_bump_ms                  = 10800000
  allow_user_auto_start             = true
  allow_user_auto_stop              = true
  allow_user_cancel_workspace_jobs  = false
  auto_start_permitted_days_of_week = ["friday", "monday", "saturday", "sunday", "thursday", "tuesday", "wednesday"]
  auto_stop_requirement = {
    days_of_week = ["sunday"]
    weeks        = 1
  }
  default_ttl_ms                 = 28800000
  deprecation_message            = null
  failure_ttl_ms                 = 604800000
  require_active_version         = true
  time_til_dormant_autodelete_ms = 7776000000
  time_til_dormant_ms            = 8640000000
}
