/*
 Copyright SecureKey Technologies Inc. All Rights Reserved.

 SPDX-License-Identifier: Apache-2.0
*/

package localkms

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/google/tink/go/aead"
	"github.com/google/tink/go/keyset"
	"github.com/google/tink/go/mac"
	commonpb "github.com/google/tink/go/proto/common_go_proto"
	ecdsapb "github.com/google/tink/go/proto/ecdsa_go_proto"
	tinkpb "github.com/google/tink/go/proto/tink_go_proto"
	"github.com/google/tink/go/signature"

	"github.com/hyperledger/aries-framework-go/pkg/crypto/tinkcrypto/primitive/composite/ecdh1pu"
	"github.com/hyperledger/aries-framework-go/pkg/crypto/tinkcrypto/primitive/composite/ecdhes"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/hyperledger/aries-framework-go/pkg/kms/localkms/internal/keywrapper"
	"github.com/hyperledger/aries-framework-go/pkg/secretlock"
	"github.com/hyperledger/aries-framework-go/pkg/storage"
)

const (
	// Namespace is the keystore's DB storage namespace
	Namespace = "kmsdb"

	ecdsaPrivateKeyTypeURL = "type.googleapis.com/google.crypto.tink.EcdsaPrivateKey"
)

// package localkms is the default KMS service implementation of pkg/kms.KeyManager. It uses Tink keys to support the
// default Crypto implementation, pkg/crypto/tinkcrypto, and stores these keys in the format understood by Tink. It also
// uses a secretLock service to protect private key material in the storage.

// LocalKMS implements kms.KeyManager to provide key management capabilities using a local db.
// It uses an underlying secret lock service (default local secretLock) to wrap (encrypt) keys
// prior to storing them.
type LocalKMS struct {
	secretLock       secretlock.Service
	masterKeyURI     string
	store            storage.Store
	masterKeyEnvAEAD *aead.KMSEnvelopeAEAD
}

// New will create a new (local) KMS service
func New(masterKeyURI string, p kms.Provider) (*LocalKMS, error) {
	store, err := p.StorageProvider().OpenStore(Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to ceate local kms: %w", err)
	}

	secretLock := p.SecretLock()

	kw, err := keywrapper.New(secretLock, masterKeyURI)
	if err != nil {
		return nil, err
	}

	// create a KMSEnvelopeAEAD instance to wrap/unwrap keys managed by LocalKMS
	masterKeyEnvAEAD := aead.NewKMSEnvelopeAEAD(*aead.AES256GCMKeyTemplate(), kw)

	return &LocalKMS{
			store:            store,
			secretLock:       secretLock,
			masterKeyURI:     masterKeyURI,
			masterKeyEnvAEAD: masterKeyEnvAEAD},
		nil
}

// Create a new key/keyset/key handle for the type kt
// Returns:
//  - keyID of the handle
//  - handle instance (to private key)
//  - error if failure
func (l *LocalKMS) Create(kt kms.KeyType) (string, interface{}, error) {
	if kt == "" {
		return "", nil, fmt.Errorf("failed to create new key, missing key type")
	}

	keyTemplate, err := getKeyTemplate(kt)
	if err != nil {
		return "", nil, err
	}

	kh, err := keyset.NewHandle(keyTemplate)
	if err != nil {
		return "", nil, err
	}

	kID, err := l.storeKeySet(kh)
	if err != nil {
		return "", nil, err
	}

	return kID, kh, nil
}

// Get key handle for the given keyID
// Returns:
//  - handle instance (to private key)
//  - error if failure
func (l *LocalKMS) Get(keyID string) (interface{}, error) {
	return l.getKeySet(keyID)
}

// Rotate a key referenced by keyID and return a new handle of a keyset including old key and
// new key with type kt. It also returns the updated keyID as the first return value
// Returns:
//  - new KeyID // TODO remove this return from Rotate() - #1837
//  - handle instance (to private key)
//  - error if failure
// TODO remove new keyID creation from Rotate(), it should re use the same keyID - #1837
func (l *LocalKMS) Rotate(kt kms.KeyType, keyID string) (string, interface{}, error) {
	kh, err := l.getKeySet(keyID)
	if err != nil {
		return "", nil, err
	}

	keyTemplate, err := getKeyTemplate(kt)
	if err != nil {
		return "", nil, err
	}

	km := keyset.NewManagerFromHandle(kh)

	err = km.Rotate(keyTemplate)
	if err != nil {
		return "", nil, err
	}

	updatedKH, err := km.Handle()

	if err != nil {
		return "", nil, err
	}

	err = l.store.Delete(keyID)
	if err != nil {
		return "", nil, err
	}

	newID, err := l.storeKeySet(updatedKH)
	if err != nil {
		return "", nil, err
	}

	return newID, updatedKH, nil
}

// nolint:gocyclo,funlen
func getKeyTemplate(keyType kms.KeyType) (*tinkpb.KeyTemplate, error) {
	switch keyType {
	case kms.AES128GCMType:
		return aead.AES128GCMKeyTemplate(), nil
	case kms.AES256GCMNoPrefixType:
		// RAW (to support keys not generated by Tink)
		return aead.AES256GCMNoPrefixKeyTemplate(), nil
	case kms.AES256GCMType:
		return aead.AES256GCMKeyTemplate(), nil
	case kms.ChaCha20Poly1305Type:
		return aead.ChaCha20Poly1305KeyTemplate(), nil
	case kms.XChaCha20Poly1305Type:
		return aead.XChaCha20Poly1305KeyTemplate(), nil
	case kms.ECDSAP256TypeDER:
		return signature.ECDSAP256KeyWithoutPrefixTemplate(), nil
	case kms.ECDSAP384TypeDER:
		return signature.ECDSAP384KeyWithoutPrefixTemplate(), nil
	case kms.ECDSAP521TypeDER:
		return signature.ECDSAP521KeyWithoutPrefixTemplate(), nil
	case kms.ECDSAP256TypeIEEEP1363:
		// JWS keys should sign using IEEE_P1363 format only (not DER format)
		return createECDSAIEEE1363KeyTemplate(commonpb.HashType_SHA256, commonpb.EllipticCurveType_NIST_P256), nil
	case kms.ECDSAP384TypeIEEEP1363:
		return createECDSAIEEE1363KeyTemplate(commonpb.HashType_SHA384, commonpb.EllipticCurveType_NIST_P384), nil
	case kms.ECDSAP521TypeIEEEP1363:
		return createECDSAIEEE1363KeyTemplate(commonpb.HashType_SHA512, commonpb.EllipticCurveType_NIST_P521), nil
	case kms.ED25519Type:
		return signature.ED25519KeyWithoutPrefixTemplate(), nil
	case kms.HMACSHA256Tag256Type:
		return mac.HMACSHA256Tag256KeyTemplate(), nil
	case kms.ECDHES256AES256GCMType:
		return ecdhes.ECDHES256KWAES256GCMKeyTemplate(), nil
	case kms.ECDHES384AES256GCMType:
		return ecdhes.ECDHES384KWAES256GCMKeyTemplate(), nil
	case kms.ECDHES521AES256GCMType:
		return ecdhes.ECDHES521KWAES256GCMKeyTemplate(), nil
	case kms.ECDH1PU256AES256GCMType:
		// Keys created by ECDH1PU templates should be used only to be persisted in the KMS. To execute primitives,
		// one must add the sender public key (on the recipient side using ecdh1pu.AddSenderKey()) or the recipient(s)
		// public key(s) (on the sender side using ecdh1pu.AddRecipientsKeys())
		return ecdh1pu.ECDH1PU256KWAES256GCMKeyTemplate(), nil
	case kms.ECDH1PU384AES256GCMType:
		return ecdh1pu.ECDH1PU384KWAES256GCMKeyTemplate(), nil
	case kms.ECDH1PU521AES256GCMType:
		return ecdh1pu.ECDH1PU521KWAES256GCMKeyTemplate(), nil
	default:
		return nil, fmt.Errorf("key type unrecognized")
	}
}

func createECDSAIEEE1363KeyTemplate(hashType commonpb.HashType, curve commonpb.EllipticCurveType) *tinkpb.KeyTemplate {
	params := &ecdsapb.EcdsaParams{
		HashType: hashType,
		Curve:    curve,
		Encoding: ecdsapb.EcdsaSignatureEncoding_IEEE_P1363,
	}
	format := &ecdsapb.EcdsaKeyFormat{Params: params}
	serializedFormat, _ := proto.Marshal(format) //nolint:errcheck

	return &tinkpb.KeyTemplate{
		TypeUrl:          ecdsaPrivateKeyTypeURL,
		Value:            serializedFormat,
		OutputPrefixType: tinkpb.OutputPrefixType_RAW,
	}
}

func (l *LocalKMS) storeKeySet(kh *keyset.Handle) (string, error) {
	buf := new(bytes.Buffer)
	jsonKeysetWriter := keyset.NewJSONWriter(buf)

	err := kh.Write(jsonKeysetWriter, l.masterKeyEnvAEAD)
	if err != nil {
		return "", err
	}

	return writeToStore(l.store, buf)
}

func writeToStore(store storage.Store, buf *bytes.Buffer, opts ...kms.PrivateKeyOpts) (string, error) {
	w := newWriter(store, opts...)

	// write buffer to localstorage
	_, err := w.Write(buf.Bytes())
	if err != nil {
		return "", err
	}

	return w.KeysetID, nil
}

func (l *LocalKMS) getKeySet(id string) (*keyset.Handle, error) {
	localDBReader := newReader(l.store, id)
	jsonKeysetReader := keyset.NewJSONReader(localDBReader)

	// Read reads the encrypted keyset handle back from the io.reader implementation
	// and decrypts it using masterKeyEnvAEAD.
	kh, err := keyset.Read(jsonKeysetReader, l.masterKeyEnvAEAD)
	if err != nil {
		return nil, err
	}

	return kh, nil
}

// ExportPubKeyBytes will fetch a key referenced by id then gets its public key in raw bytes and returns it.
// The key must be an asymmetric key.
// Returns:
//  - marshalled public key []byte
//  - error if it fails to export the public key bytes
func (l *LocalKMS) ExportPubKeyBytes(id string) ([]byte, error) {
	kh, err := l.getKeySet(id)
	if err != nil {
		return nil, err
	}

	// kh must be a private asymmetric key in order to extract its public key
	pubKH, err := kh.Public()
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	pubKeyWriter := NewWriter(buf)

	err = pubKH.WriteWithNoSecrets(pubKeyWriter)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// PubKeyBytesToHandle will create and return a key handle for pubKey of type kt
// it returns an error if it failed creating the key handle
// Note: The key handle created is not stored in the KMS, it's only useful to execute the crypto primitive
// associated with it.
func (l *LocalKMS) PubKeyBytesToHandle(pubKey []byte, kt kms.KeyType) (interface{}, error) {
	return publicKeyBytesToHandle(pubKey, kt)
}

// ImportPrivateKey will import privKey into the KMS storage for the given keyType then returns the new key id and
// the newly persisted Handle.
// 'privKey' possible types are: *ecdsa.PrivateKey and ed25519.PrivateKey
// 'keyType' possible types are signing key types only (ECDSA keys or Ed25519)
// 'opts' allows setting the keysetID of the imported key using WithKeyID() option. If the ID is already used,
// then an error is returned.
// Returns:
//  - keyID of the handle
//  - handle instance (to private key)
//  - error if import failure (key empty, invalid, doesn't match keyType, unsupported keyType or storing key failed)
func (l *LocalKMS) ImportPrivateKey(privKey interface{}, kt kms.KeyType,
	opts ...kms.PrivateKeyOpts) (string, interface{}, error) {
	switch pk := privKey.(type) {
	case *ecdsa.PrivateKey:
		return l.importECDSAKey(pk, kt, opts...)
	case ed25519.PrivateKey:
		return l.importEd25519Key(pk, kt, opts...)
	default:
		return "", nil, fmt.Errorf("import private key does not support this key type or key is public")
	}
}
