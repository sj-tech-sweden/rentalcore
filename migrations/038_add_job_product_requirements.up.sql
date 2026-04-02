-- Migration: Add job_product_requirements table
-- Description: Stores the required quantity of each product for a job without
--              pre-assigning specific devices. Actual device assignment happens
--              in warehousecore when scanning.

CREATE TABLE IF NOT EXISTS job_product_requirements (
    requirement_id SERIAL PRIMARY KEY,
    job_id         INT NOT NULL,
    product_id     INT NOT NULL,
    quantity       INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    CONSTRAINT fk_jpr_job     FOREIGN KEY (job_id)     REFERENCES jobs(jobid)         ON DELETE CASCADE,
    CONSTRAINT fk_jpr_product FOREIGN KEY (product_id) REFERENCES products(productid) ON DELETE CASCADE,
    CONSTRAINT uq_jpr_job_product UNIQUE (job_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_jpr_job_id     ON job_product_requirements (job_id);
CREATE INDEX IF NOT EXISTS idx_jpr_product_id ON job_product_requirements (product_id);
