// Protected migration streaming only. Never writes pixels, keys, or raw tar.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {spawn, spawnSync} from 'node:child_process';
import {Transform} from 'node:stream';
import {pipeline} from 'node:stream/promises';
import {fileURLToPath} from 'node:url';

const expectedCipher = '4c4b7cd1e4b45d9cdc3a7aee3cc31e136702eb44b3c7b0bc5f91de21f365bc0d';
const [sourceFile, outputDirectory, receiptFile, python, ...extra] = process.argv.slice(2);
let key, reader;
try {
  if (extra.length || ![sourceFile, outputDirectory, receiptFile, python].every(v => v && path.isAbsolute(v))) throw Error();
  const receipt = JSON.parse(fs.readFileSync(receiptFile, 'utf8'));
  if (receipt.format !== 'birdtie-backup-v1' || receipt.kind !== 'media' || receipt.source !== 'civu-hangzhou' ||
      receipt.encryption !== 'AES-256-GCM' || receipt.ciphertextSHA256 !== expectedCipher ||
      ![receipt.archive, receipt.keyFile].every(v => typeof v === 'string' && path.basename(v) === v && !v.includes('\\'))) throw Error();
  const root = path.dirname(receiptFile), cipherPath = path.join(root, receipt.archive);
  const cipherHash = crypto.createHash('sha256');
  let cipherBytes = 0;
  for await (const chunk of fs.createReadStream(cipherPath)) { cipherHash.update(chunk); cipherBytes += chunk.length; }
  if (cipherHash.digest('hex') !== expectedCipher || cipherBytes !== receipt.cipherBytes) throw Error();
  const unwrapProgram = "Add-Type -AssemblyName System.Security; $wrapped=[Convert]::FromBase64String([Console]::In.ReadToEnd().Trim()); $raw=[System.Security.Cryptography.ProtectedData]::Unprotect($wrapped,$null,[System.Security.Cryptography.DataProtectionScope]::CurrentUser); [Console]::Out.Write([Convert]::ToBase64String($raw))";
  const unwrapped = spawnSync('C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe',
    ['-NoProfile', '-NonInteractive', '-Command', unwrapProgram],
    {input:fs.readFileSync(path.join(root, receipt.keyFile), 'utf8'), encoding:'utf8', windowsHide:true});
  if (unwrapped.status !== 0) throw Error();
  key = Buffer.from(unwrapped.stdout.trim(), 'base64');
  unwrapped.stdout = ''; // Do not include key-recovery buffers in diagnostics.
  if (key.length !== 32) throw Error();
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, Buffer.from(receipt.iv, 'base64'));
  decipher.setAAD(Buffer.from(receipt.aad, 'base64'));
  decipher.setAuthTag(Buffer.from(receipt.authTag, 'base64'));
  const plainHash = crypto.createHash('sha256');
  let plainBytes = 0;
  const hashPlain = new Transform({transform(chunk, _, cb) { plainHash.update(chunk); plainBytes += chunk.length; cb(null, chunk); }});
  const script = path.join(path.dirname(fileURLToPath(import.meta.url)), 'match_civu_media_archive.py');
  reader = spawn(python, [script, '--source-private', sourceFile, '--output-directory', outputDirectory],
    {windowsHide:true, stdio:['pipe', 'pipe', 'pipe']});
  let stdout = '', stderr = '';
  reader.stdout.on('data', chunk => { stdout = (stdout + chunk.toString()).slice(-20000); });
  reader.stderr.on('data', chunk => { stderr = (stderr + chunk.toString()).slice(-20000); });
  const closed = new Promise((resolve, reject) => { reader.once('error', reject); reader.once('close', resolve); });
  await pipeline(fs.createReadStream(cipherPath), decipher, hashPlain, reader.stdin);
  const exit = await closed, digest = plainHash.digest('hex');
  if (exit !== 0 || digest !== receipt.plaintextSHA256 || plainBytes !== receipt.plainBytes || stderr.length) throw Error();
  const matchPath = path.join(outputDirectory, 'matched.private.json');
  const raw = fs.readFileSync(matchPath), result = JSON.parse(raw.toString('utf8'));
  if (result.archivePlaintextSHA256 !== digest || result.archiveBytes !== plainBytes ||
      result.schemaVersion !== 'birdtie-civu-media-match-v1' || result.streamAuthentication !== 'NOT_VERIFIED') throw Error();
  const seal = {format:'birdtie-civu-media-authenticated-match-v1', checkedAtUTC:new Date().toISOString(),
    aesGcmAuthenticated:true, ciphertextSHA256:expectedCipher, plaintextSHA256:digest,
    matcherFileSHA256:crypto.createHash('sha256').update(raw).digest('hex'), readerExit:exit,
    summary:result.summary, archiveMembers:result.archiveMembers, archiveFiles:result.archiveFiles,
    extractedFiles:0, databaseWrites:0, sourceWrites:0, publicationRights:'NOT_AUTHORIZED', scanner:'NOT_RUN', exifCleaning:'NOT_RUN'};
  fs.writeFileSync(path.join(outputDirectory, 'authenticated-seal.json'), JSON.stringify(seal, null, 2)+'\n', {flag:'wx'});
  // Only aggregate facts; never relay the raw reader result or stderr.
  console.log(JSON.stringify({status:'AUTHENTICATED_PRIVATE_OBJECT_MATCH', ...seal}));
} catch {
  reader?.kill();
  console.log(JSON.stringify({status:'REJECTED', reason:'AUTHENTICATED_MEDIA_MATCH_FAILED',
    accepted:false, extractedFiles:0, publicationRights:'NOT_AUTHORIZED'}));
  process.exitCode = 2;
} finally { key?.fill(0); }
