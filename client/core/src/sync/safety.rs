use std::fs::{File, Metadata, OpenOptions};
use std::io;
use std::path::{Component, Path};

use crate::error::CoreError;

pub(super) fn is_link(metadata: &Metadata) -> bool {
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        if metadata.file_attributes() & 0x400 != 0 {
            return true;
        }
    }
    metadata.file_type().is_symlink()
}

pub(super) fn create_root(root: &Path) -> Result<(), CoreError> {
    match std::fs::symlink_metadata(root) {
        Ok(metadata) if metadata.is_dir() && !is_link(&metadata) => Ok(()),
        Ok(_) => Err(CoreError::Sync(format!(
            "небезопасный каталог: {}",
            root.display()
        ))),
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            std::fs::create_dir_all(root)?;
            let metadata = std::fs::symlink_metadata(root)?;
            if metadata.is_dir() && !is_link(&metadata) {
                Ok(())
            } else {
                Err(CoreError::Sync(format!(
                    "небезопасный каталог: {}",
                    root.display()
                )))
            }
        }
        Err(error) => Err(error.into()),
    }
}

pub(super) fn check_parents(root: &Path, dest: &Path, create: bool) -> Result<(), CoreError> {
    let parent = dest
        .parent()
        .ok_or_else(|| CoreError::Sync("у файла нет родительского каталога".into()))?;
    let relative = parent
        .strip_prefix(root)
        .map_err(|_| CoreError::Sync(format!("путь вне каталога: {}", dest.display())))?;
    if relative
        .components()
        .any(|part| !matches!(part, Component::Normal(_)))
    {
        return Err(CoreError::Sync(format!(
            "путь вне каталога: {}",
            dest.display()
        )));
    }
    match std::fs::symlink_metadata(root) {
        Ok(metadata) if metadata.is_dir() && !is_link(&metadata) => (),
        Ok(_) => {
            return Err(CoreError::Sync(format!(
                "небезопасный каталог: {}",
                root.display()
            )))
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound && !create => return Ok(()),
        Err(error) => return Err(error.into()),
    }
    let mut current = root.to_path_buf();
    for part in relative.components() {
        current.push(part);
        match std::fs::symlink_metadata(&current) {
            Ok(metadata) if is_link(&metadata) => {
                return Err(CoreError::Sync(format!(
                    "путь проходит через ссылку (symlink/reparse point): {}",
                    current.display()
                )))
            }
            Ok(metadata) if metadata.is_dir() => {
                #[cfg(windows)]
                if current
                    .file_name()
                    .is_some_and(|name| name.to_string_lossy().to_uppercase() != ".LAMINARA")
                    && same_file::is_same_file(&current, root.join(".laminara")).unwrap_or(false)
                {
                    return Err(CoreError::Sync(format!(
                        "путь указывает на служебный каталог: {}",
                        current.display()
                    )));
                }
            }
            Ok(_) => {
                return Err(CoreError::Sync(format!(
                    "ожидался каталог: {}",
                    current.display()
                )))
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound && create => {
                std::fs::create_dir(&current)?
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(error) => return Err(error.into()),
        }
    }
    Ok(())
}

fn check_regular(metadata: &Metadata) -> io::Result<()> {
    if !metadata.is_file() || is_link(metadata) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "ожидался обычный файл без ссылки",
        ));
    }
    Ok(())
}

pub(super) fn regular_metadata(path: &Path) -> io::Result<Option<Metadata>> {
    match std::fs::symlink_metadata(path) {
        Ok(metadata) => {
            check_regular(&metadata)?;
            Ok(Some(metadata))
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error),
    }
}

fn options() -> OpenOptions {
    let mut options = OpenOptions::new();
    options.read(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.custom_flags(libc::O_NOFOLLOW);
    }
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        options.custom_flags(0x0020_0000);
    }
    options
}

pub(super) fn read_file(path: &Path) -> io::Result<File> {
    regular_metadata(path)?.ok_or_else(|| io::Error::from(io::ErrorKind::NotFound))?;
    let file = options().open(path)?;
    check_regular(&file.metadata()?)?;
    Ok(file)
}

fn require_single_link(file: &File) -> io::Result<()> {
    #[cfg(unix)]
    let links = {
        use std::os::unix::fs::MetadataExt;
        file.metadata()?.nlink()
    };
    #[cfg(windows)]
    let links = {
        let handle = winapi_util::Handle::from_file(file.try_clone()?);
        winapi_util::file::information(&handle)?.number_of_links()
    };
    #[cfg(not(any(unix, windows)))]
    let links = return Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "проверка hardlink не поддерживается",
    ));
    if links != 1 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "файл имеет hardlink",
        ));
    }
    Ok(())
}

pub(super) fn read_private(path: &Path) -> io::Result<File> {
    let file = read_file(path)?;
    require_single_link(&file)?;
    Ok(file)
}

pub(super) fn open_partial(path: &Path) -> io::Result<File> {
    regular_metadata(path)?;
    let file = options()
        .write(true)
        .create(true)
        .truncate(false)
        .open(path)?;
    check_regular(&file.metadata()?)?;
    require_single_link(&file)?;
    Ok(file)
}

pub(super) fn remove_file(path: &Path) -> io::Result<bool> {
    let Some(metadata) = regular_metadata(path)? else {
        return Ok(false);
    };
    match std::fs::remove_file(path) {
        Ok(()) => Ok(true),
        Err(error) => {
            #[cfg(windows)]
            if error.kind() == io::ErrorKind::PermissionDenied && metadata.permissions().readonly()
            {
                read_private(path)?;
                let mut permissions = metadata.permissions();
                permissions.set_readonly(false);
                std::fs::set_permissions(path, permissions)?;
                std::fs::remove_file(path)?;
                return Ok(true);
            }
            #[cfg(not(windows))]
            let _ = metadata;
            Err(error)
        }
    }
}

pub(super) fn same_entry(left: &Path, right: &Path) -> io::Result<bool> {
    let same = match same_file::is_same_file(left, right) {
        Ok(same) => same,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(false),
        Err(error) => return Err(error),
    };
    if !same {
        return Ok(false);
    }
    let (Some(left_parent), Some(right_parent)) = (left.parent(), right.parent()) else {
        return Ok(false);
    };
    if !same_file::is_same_file(left_parent, right_parent)? {
        return Ok(false);
    }
    if left.file_name() == right.file_name() {
        return Ok(true);
    }
    let mut left_exists = false;
    let mut right_exists = false;
    for entry in std::fs::read_dir(left_parent)? {
        let name = entry?.file_name();
        left_exists |= Some(name.as_os_str()) == left.file_name();
        right_exists |= Some(name.as_os_str()) == right.file_name();
    }
    Ok(!(left_exists && right_exists))
}
