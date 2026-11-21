package push

import "github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"

func PublicKey(privateKey string) string { return pushsupport.PublicKey(privateKey) }
