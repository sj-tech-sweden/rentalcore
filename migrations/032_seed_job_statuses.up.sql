INSERT INTO status (statusid, status) VALUES 
(1, 'Draft'),
(2, 'Confirmed'),
(3, 'Active'),
(4, 'Completed'),
(5, 'Cancelled')
ON CONFLICT (statusid) DO NOTHING;

-- Update sequence to ensure next insert works
SELECT setval('status_statusid_seq', (SELECT MAX(statusid) FROM status));
