-- Rollback removes the private derivative store; it is not a production purge.
DROP TRIGGER private_moment_images_retire ON moments;
DROP FUNCTION birdtie_retire_private_moment_images();
DROP TABLE private_moment_images;
