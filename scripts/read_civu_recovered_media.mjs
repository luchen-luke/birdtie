// Explicit protected recovery extraction. No keys or raw tar are written.
// Only hash-named31 original selected images are saved outside the repository.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {spawn, spawnSync} from 'node:child_process';
import {Transform} from 'node:stream';
import {pipeline} from 'node:stream/promises';
import {fileURLToPath} from 'node:url';

const expectedCipher = '4c4b7cd1e4b45d9cdc3a7aee3cc31e136702eb44b3c7b0bc5f91de21f365bc0d';
const [sourceFile, outputDirectory, receiptFile, python, matchedFile, extractScript, ...extra] = process.argv.slice(2);
let key, reader;
try {
  if (extra.length || ![sourceFile, outputDirectory, receiptFile, python, matchedFile, extractScript].every(v => v && path.isAbsolute(v))) throw Error();
  const official=path.resolve('D:/Project/birdtie').toLowerCase();
  const output=path.resolve(outputDirectory).toLowerCase();
  if(output===official || output.startsWith(official+path.sep.toLowerCase()) || !fs.statSync(outputDirectory).isDirectory())throw Error();
  for(let current=path.resolve(outputDirectory);;current=path.dirname(current)){
    if(fs.lstatSync(current).isSymbolicLink())throw Error();
    if(path.dirname(current)===current)break;
  }
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
  const script = extractScript;
  reader = spawn(python, [script, sourceFile, matchedFile, outputDirectory],
    {windowsHide:true, stdio:['pipe', 'pipe', 'pipe']});
  let stdout = '', stderr = '';
  reader.stdout.on('data', chunk => { stdout = (stdout + chunk.toString()).slice(-20000); });
  reader.stderr.on('data', chunk => { stderr = (stderr + chunk.toString()).slice(-20000); });
  const closed = new Promise((resolve, reject) => { reader.once('error', reject); reader.once('close', resolve); });
  await pipeline(fs.createReadStream(cipherPath), decipher, hashPlain, reader.stdin);
  const exit = await closed, digest = plainHash.digest('hex');
  if (exit !== 0 || digest !== receipt.plaintextSHA256 || plainBytes !== receipt.plainBytes || stderr.length) throw Error();
  const result = JSON.parse(stdout);
  if(result.extractedFiles !== 31 || result.archivePlaintextSHA256 !== digest || result.archiveBytes !== plainBytes) throw Error();
  const seal={status:'AUTHENTICATED_RECOVERED_PRIVATE_IMAGES',aesGcmAuthenticated:true,ciphertextSHA256:expectedCipher,plaintextSHA256:digest,extractedFiles:31,databaseWrites:0,sourceWrites:0};
  fs.writeFileSync(path.join(outputDirectory,'extraction-authenticated.json'),JSON.stringify(seal,null,2)+'\n',{flag:'wx'});
  console.log(JSON.stringify(seal));
} catch {
  reader?.kill();
  console.log(JSON.stringify({status:'REJECTED', reason:'AUTHENTICATED_MEDIA_MATCH_FAILED',
    accepted:false, extractedFiles:0, publicationRights:'NOT_AUTHORIZED'}));
  process.exitCode = 2;
} finally { key?.fill(0); }
