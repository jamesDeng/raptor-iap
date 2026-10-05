variable "account_id" { type = string }
variable "fingerprints" {
  type        = set(string)
  description = "Independently verified current GitHub issuer certificate CA fingerprints, never guessed."
  validation {
    condition     = length(var.fingerprints) > 0 && alltrue([for f in var.fingerprints : can(regex("^[0-9a-fA-F]{40}$", f))])
    error_message = "At least one verified 40-character SHA1 certificate fingerprint is required."
  }
}
