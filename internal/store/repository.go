// Package store is responsible to interact with the storage backend used for bookmarks
// this is done by implementing a repository for the datbase

package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bihe/bookmarks/internal"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
)

/* migrated from dotnet

TODO

	Task<List<BookmarkEntity>> GetBookmarksByPath(string path, string username);
	Task<List<BookmarkEntity>> GetBookmarksByPathStart(string startPath, string username);
	Task<List<BookmarkEntity>> GetBookmarksByName(string name, string username);
	Task<List<NodeCount>> GetChildCountOfPath(string path, string username);
	Task<List<BookmarkEntity>> GetMostRecentBookmarks(string username, int limit);

	Task<BookmarkEntity> GetFolderByPath(string path, string username);


	Task<bool> DeletePath(string path, string username);

DONE

	Task<List<BookmarkEntity>> GetAllBookmarks(string username);

	Task<(bool result, T value)> InUnitOfWorkAsync<T>(Func<Task<(bool result,T value)>> atomicOperation);
	Task<BookmarkEntity> Create(BookmarkEntity item);
	Task<BookmarkEntity> Update(BookmarkEntity item);
	Task<bool> Delete(BookmarkEntity item);

	Task<BookmarkEntity> GetBookmarkById(string id, string username);

*/

// Repository defines methods to interact with a store
type Repository interface {
	InUnitOfWork(fn func(repo Repository) error) error
	Create(item Bookmark) (Bookmark, error)
	Update(item Bookmark) (Bookmark, error)
	Delete(item Bookmark) error

	GetAllBookmarks(username string) ([]Bookmark, error)

	GetBookmarkById(id, username string) (Bookmark, error)
}

// Create a new repository
func Create(db *gorm.DB) Repository {
	return &dbRepository{
		transient: db,
		shared:    nil,
	}
}

// --------------------------------------------------------------------------
// Implementation
// --------------------------------------------------------------------------

type dbRepository struct {
	transient *gorm.DB
	shared    *gorm.DB
}

// InUnitOfWork uses a trancation to execute the contents of the supplied function
func (r *dbRepository) InUnitOfWork(fn func(repo Repository) error) error {
	return r.con().Transaction(func(tx *gorm.DB) error {
		// be sure the stop recursion here
		if r.shared != nil {
			return fmt.Errorf("a shared connection/transaction is already available, will not start a new one")
		}
		return fn(&dbRepository{
			transient: r.transient,
			shared:    tx, // the transaction is used as the shared connection
		})
	})
}

// GetAllBookmarks retrieves all available bookmarks for the given user
func (r *dbRepository) GetAllBookmarks(username string) ([]Bookmark, error) {
	var bookmarks []Bookmark
	h := r.con().Order("sort_order").Order("display_name").Where(&Bookmark{UserName: username}).Find(&bookmarks)
	return bookmarks, h.Error
}

// GetBookmarkById returns the bookmark specified by the given id - for the user
func (r *dbRepository) GetBookmarkById(id, username string) (Bookmark, error) {
	var bookmark Bookmark
	h := r.con().Where(&Bookmark{ID: id, UserName: username}).First(&bookmark)
	return bookmark, h.Error
}

// Create is used to save a new bookmark entry
func (r *dbRepository) Create(item Bookmark) (Bookmark, error) {
	var (
		err       error
		hierarchy []string
	)

	if item.Path == "" {
		return Bookmark{}, fmt.Errorf("path is empty")
	}

	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	item.Created = time.Now().UTC()

	internal.LogFunction("store.Create").Debugf("create new bookmark item: %+v", item)

	// if we create a new bookmark item using a specific path we need to ensure that
	// the parent-path is available. as this is a hierarchical structure this is quite tedious
	// the solution is to query the whole hierarchy and check if the given path is there

	if item.Path != "/" {
		hierarchy, err = r.availablePaths(item.UserName)
		if err != nil {
			return Bookmark{}, err
		}
		found := false
		for _, h := range hierarchy {
			if h == item.Path {
				found = true
				break
			}
		}
		if !found {
			internal.LogFunction("store.Create").Warnf("cannot create the bookmark '%+v' because the parent path '%s' is not available!", item, item.Path)
			return Bookmark{}, fmt.Errorf("cannot create item because of missing path hierarchy '%s'", item.Path)
		}
	}

	if h := r.con().Create(&item); h.Error != nil {
		return Bookmark{}, h.Error
	}

	// this entry (either node or folder) was created with a given path. increment the number of child-elements
	// for this given path, and update the "parent" directory entry.
	// exception: if the path is ROOT, '/' no update needs to be done, because no dedicated ROOT, '/' entry
	if item.Path != "/" {
		err = r.calcChildCount(item.Path, item.UserName, func(c int) int {
			return c + 1
		})
		if err != nil {
			return Bookmark{}, fmt.Errorf("could not update the child-count for '%s': %v", item.Path, err)
		}
	}

	return item, nil
}

// Update changes an existing bookmark item
func (r *dbRepository) Update(item Bookmark) (Bookmark, error) {
	var (
		err       error
		hierarchy []string
		bm        Bookmark
	)

	if item.Path == "" {
		return Bookmark{}, fmt.Errorf("path is empty")
	}

	h := r.con().Where(&Bookmark{ID: item.ID, UserName: item.UserName}).First(&bm)
	if h.Error != nil {
		return Bookmark{}, fmt.Errorf("cannot get bookmark by id '%s': %v", item.ID, h.Error)
	}

	internal.LogFunction("store.Update").Debugf("update bookmark item: %+v", item)

	// if we create a new bookmark item using a specific path we need to ensure that
	// the parent-path is available. as this is a hierarchical structure this is quite tedious
	// the solution is to query the whole hierarchy and check if the given path is there

	if item.Path != "/" {
		hierarchy, err = r.availablePaths(item.UserName)
		if err != nil {
			return Bookmark{}, err
		}
		found := false
		for _, h := range hierarchy {
			if h == item.Path {
				found = true
				break
			}
		}
		if !found {
			internal.LogFunction("store.Update").Warnf("cannot update the bookmark '%+v' because the parent path '%s' is not available!", item, item.Path)
			return Bookmark{}, fmt.Errorf("cannot update item because of missing path hierarchy '%s'", item.Path)
		}
	}

	now := time.Now().UTC()
	bm.Modified = &now
	bm.DisplayName = item.DisplayName
	bm.Path = item.Path
	bm.SortOrder = item.SortOrder
	bm.URL = item.URL
	bm.Favicon = item.Favicon
	bm.AccessCount = item.AccessCount
	bm.ChildCount = item.ChildCount

	h = r.con().Save(&bm)
	if h.Error != nil {
		return Bookmark{}, fmt.Errorf("cannot update bookmark with id '%s': %v", item.ID, h.Error)
	}
	return bm, nil
}

// Delete removes the bookmark identified by id
func (r *dbRepository) Delete(item Bookmark) error {
	var (
		bm  Bookmark
		err error
	)

	h := r.con().Where(&Bookmark{ID: item.ID, UserName: item.UserName}).First(&bm)
	if h.Error != nil {
		return fmt.Errorf("cannot get bookmark by id '%s': %v", item.ID, h.Error)
	}

	internal.LogFunction("store.Delete").Debugf("delete bookmark item: %+v", item)

	// one item is removed from a given path, decrement the child-count for
	// the folder / path this item is located in
	if item.Path != "/" {
		err = r.calcChildCount(item.Path, item.UserName, func(c int) int {
			return c - 1
		})
		if err != nil {
			return fmt.Errorf("could not update the child-count for '%s': %v", item.Path, err)
		}
	}

	h = r.con().Delete(&bm)
	if h.Error != nil {
		return fmt.Errorf("cannot delete bookmark by id '%s': %v", item.ID, h.Error)
	}
	return nil
}

// --------------------------------------------------------------------------
// internal logic / helpers
// --------------------------------------------------------------------------

func (r *dbRepository) con() *gorm.DB {
	if r.shared != nil {
		return r.shared
	}
	if r.transient == nil {
		panic("no database connection is available")
	}
	return r.transient
}

const nativeHierarchyQuery = `SELECT '/' as path

UNION ALL

SELECT a.path || '/' || a.display_name FROM (

    SELECT
        CASE ii.path
            WHEN '/' THEN ''
            ELSE ii.path
        END AS path, ii.display_name
    FROM BOOKMARKS ii WHERE
        ii.type = ? AND ii.user_name = ?
) a
GROUP BY a.path || '/' || a.display_name`

func (r *dbRepository) availablePaths(username string) (paths []string, err error) {
	var (
		rows *sql.Rows
	)

	rows, err = r.con().Raw(nativeHierarchyQuery, Folder, username).Rows() // (*sql.Rows, error)
	defer func(ro *sql.Rows) {
		if ro != nil {
			err = ro.Close()
		}
	}(rows)

	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (r *dbRepository) calcChildCount(path, username string, fn func(i int) int) error {
	// the supplied path is of the form
	// /A/B/C => get the entry C (which is a folder) and inc/dec the child-count
	parentPath, parentName, ok := pathAndFolder(path)
	if !ok {
		return fmt.Errorf("invalid path encountered '%s'", path)
	}
	var bm Bookmark
	if h := r.con().Where(&Bookmark{
		UserName:    username,
		Path:        parentPath,
		Type:        Folder,
		DisplayName: parentName}).First(&bm); h.Error != nil {
		return fmt.Errorf("could not get parent item '%s, %s'", parentPath, parentName)
	}

	// update the found item
	count := fn(bm.ChildCount)
	if h := r.con().Model(&bm).Update("child_count", count); h.Error != nil {
		return fmt.Errorf("cannot update item '%+v': %v", bm, h.Error)
	}
	return nil
}

func pathAndFolder(fullPath string) (path string, folder string, valid bool) {
	i := strings.LastIndex(fullPath, "/")
	if i == -1 {
		return
	}

	parent := fullPath[0:i]
	if i == 0 || parent == "" {
		parent = "/"
	}

	name := fullPath[i+1:]

	return parent, name, true
}