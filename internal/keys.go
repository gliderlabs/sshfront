package internal

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"golang.org/x/crypto/ssh"
)

func signerFromBlock(block *pem.Block) (ssh.Signer, error) {
	var key interface{}
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "DSA PRIVATE KEY":
		key, err = ssh.ParseDSAPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type %q", block.Type)
	}
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	return signer, nil
}

func decodePemBlocks(pemData []byte) []*pem.Block {
	var blocks []*pem.Block
	var block *pem.Block
	for {
		block, pemData = pem.Decode(pemData)
		if block == nil {
			return blocks
		}
		blocks = append(blocks, block)
	}
}

func SetupHostKey(config *ssh.ServerConfig) {
	var signers []ssh.Signer
	if keyEnv := os.Getenv("SSH_PRIVATE_KEYS"); keyEnv != "" {
		for _, block := range decodePemBlocks([]byte(keyEnv)) {
			signer, _ := signerFromBlock(block)
			if signer != nil {
				signers = append(signers, signer)
			}
		}
	}
	if *HostKey != "" {
		pemBytes, err := ioutil.ReadFile(*HostKey)
		if err != nil {
			Debug("host key file error:", err)
		}
		for _, block := range decodePemBlocks(pemBytes) {
			signer, _ := signerFromBlock(block)
			if signer != nil {
				signers = append(signers, signer)
			}
		}
	}
	if len(signers) > 0 {
		for _, signer := range signers {
			config.AddHostKey(signer)
		}
	} else {
		Debug("no host key provided, generating host key")
		key, err := rsa.GenerateKey(rand.Reader, 768)
		if err != nil {
			log.Fatalln("failed key generate:", err)
		}
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			log.Fatalln("failed signer:", err)
		}
		config.AddHostKey(signer)
	}
}
