-- Migration: Create documents table for PostgreSQL
-- This creates the documents and digital_signatures tables required by DocumentHandler

CREATE TABLE IF NOT EXISTS documents (
    "documentID"         SERIAL PRIMARY KEY,
    entity_type          VARCHAR(20) NOT NULL CHECK (entity_type IN ('job', 'device', 'customer', 'user', 'system')),
    entity_id            VARCHAR(50) NOT NULL,
    filename             VARCHAR(255) NOT NULL,
    original_filename    VARCHAR(255) NOT NULL,
    file_path            VARCHAR(500) NOT NULL,
    file_size            BIGINT NOT NULL,
    mime_type            VARCHAR(100) NOT NULL,
    document_type        VARCHAR(20) NOT NULL CHECK (document_type IN ('contract', 'manual', 'photo', 'invoice', 'receipt', 'signature', 'other')),
    description          TEXT,
    uploaded_by          INTEGER REFERENCES users("userID") ON DELETE SET NULL,
    uploaded_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_public            BOOLEAN DEFAULT FALSE,
    version              INTEGER DEFAULT 1,
    "parent_documentID"  INTEGER REFERENCES documents("documentID") ON DELETE SET NULL,
    checksum             VARCHAR(64)
);

CREATE INDEX IF NOT EXISTS idx_documents_entity     ON documents (entity_type, entity_id, document_type);
CREATE INDEX IF NOT EXISTS idx_documents_uploaded   ON documents (uploaded_at, document_type);
CREATE INDEX IF NOT EXISTS idx_documents_uploader   ON documents (uploaded_by);

CREATE TABLE IF NOT EXISTS digital_signatures (
    "signatureID"       SERIAL PRIMARY KEY,
    "documentID"        INTEGER NOT NULL REFERENCES documents("documentID") ON DELETE CASCADE,
    signer_name         VARCHAR(100) NOT NULL,
    signer_email        VARCHAR(100),
    signer_role         VARCHAR(50),
    signature_data      TEXT NOT NULL,
    signed_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address          VARCHAR(45),
    verification_code   VARCHAR(100),
    is_verified         BOOLEAN DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_signatures_document ON digital_signatures ("documentID");
