#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const https = require('https');
const { execSync } = require('child_process');

const VERSION = require('../package.json').version;
const REPO = 'ziuus/goroute';

function getTarget() {
  const platform = process.platform;
  const arch = process.arch;

  let osName = platform;
  if (platform === 'win32') osName = 'windows';

  let archName = arch;
  if (arch === 'x64') archName = 'x86_64';
  else if (arch === 'arm64') archName = 'arm64';
  else {
    throw new Error(`Unsupported architecture: ${arch}`);
  }

  const ext = platform === 'win32' ? '.zip' : '.tar.gz';
  const binaryName = platform === 'win32' ? 'goroute.exe' : 'goroute';
  const fileName = `goroute_${osName}_${archName}${ext}`;
  const url = `https://github.com/${REPO}/releases/download/v${VERSION}/${fileName}`;

  return { osName, archName, binaryName, fileName, url };
}

async function download(url, dest) {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return resolve(download(res.headers.location, dest));
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Failed to download ${url}: HTTP ${res.statusCode}`));
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on('finish', () => file.close(resolve));
    }).on('error', reject);
  });
}

async function main() {
  try {
    const target = getTarget();
    const binDir = path.join(__dirname, '..', 'bin');
    if (!fs.existsSync(binDir)) {
      fs.mkdirSync(binDir, { recursive: true });
    }

    const binaryPath = path.join(binDir, target.binaryName);
    if (fs.existsSync(binaryPath)) {
      return;
    }

    const tmpArchive = path.join(binDir, target.fileName);
    console.log(`⚡ Downloading GoRoute v${VERSION} for ${target.osName}/${target.archName}...`);
    
    await download(target.url, tmpArchive);

    if (target.fileName.endsWith('.tar.gz')) {
      execSync(`tar -xzf "${tmpArchive}" -C "${binDir}"`);
    } else if (target.fileName.endsWith('.zip')) {
      execSync(`tar -xf "${tmpArchive}" -C "${binDir}"`);
    }

    if (fs.existsSync(tmpArchive)) {
      fs.unlinkSync(tmpArchive);
    }

    if (process.platform !== 'win32' && fs.existsSync(binaryPath)) {
      fs.chmodSync(binaryPath, 0o755);
    }

    console.log(`✅ GoRoute installed successfully!`);
  } catch (err) {
    // Graceful fallback: binary will be fetched on first run by bin/goroute.js
    console.warn(`Note: Could not pre-download binary during install (${err.message}). It will be resolved on first run.`);
  }
}

main();
